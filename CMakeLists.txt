cmake_minimum_required(VERSION 3.21)
project(SATSSDV-Go VERSION 0.16.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTOUIC OFF)
set(CMAKE_AUTORCC OFF)
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")

option(SATSSDV_WITH_MALGO "Build Go backend with the native miniaudio runtime" ON)
option(SATSSDV_BUILD_BACKEND_ONLY "Build only the Go backend, without Qt desktop client" OFF)

# Allow CI and developers to provide an absolute Go executable path. This is
# important for Windows builds where setup-go's PATH may not propagate into
# an MSYS2 shell used to run MinGW CMake.
if(NOT DEFINED GO_EXECUTABLE OR GO_EXECUTABLE STREQUAL "")
    find_program(GO_EXECUTABLE NAMES go go.exe REQUIRED)
endif()
if(NOT EXISTS "${GO_EXECUTABLE}")
    message(FATAL_ERROR "GO_EXECUTABLE does not exist: ${GO_EXECUTABLE}")
endif()
message(STATUS "Go executable: ${GO_EXECUTABLE}")

if(WIN32)
    set(BACKEND_NAME satssdv-backend.exe)
else()
    set(BACKEND_NAME satssdv-backend)
endif()
set(BACKEND_OUTPUT "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}/${BACKEND_NAME}")

set(GO_BUILD_TAGS "")
if(SATSSDV_WITH_MALGO)
    set(GO_BUILD_TAGS "malgo")
endif()

# Both processes are GUI applications on Windows. The Qt parent still receives
# the backend's stdout/stderr through QProcess pipes, but neither process owns
# or creates a visible console window.
set(GO_BUILD_LDFLAGS)
if(WIN32)
    list(APPEND GO_BUILD_LDFLAGS "-ldflags=-H=windowsgui")
endif()

file(GLOB_RECURSE GO_SOURCES CONFIGURE_DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/*.go")

if(GO_BUILD_TAGS)
    add_custom_command(
        OUTPUT "${BACKEND_OUTPUT}"
        COMMAND "${CMAKE_COMMAND}" -E make_directory "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}"
        COMMAND "${CMAKE_COMMAND}" -E env "CGO_ENABLED=1" "${GO_EXECUTABLE}" build -trimpath -tags "${GO_BUILD_TAGS}" ${GO_BUILD_LDFLAGS} -o "${BACKEND_OUTPUT}" .
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        DEPENDS
            "${CMAKE_CURRENT_SOURCE_DIR}/go.mod"
            "${CMAKE_CURRENT_SOURCE_DIR}/go.sum"
            ${GO_SOURCES}
        COMMENT "Building SATSSDV-Go native backend"
        VERBATIM
    )
else()
    add_custom_command(
        OUTPUT "${BACKEND_OUTPUT}"
        COMMAND "${CMAKE_COMMAND}" -E make_directory "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}"
        COMMAND "${CMAKE_COMMAND}" -E env "CGO_ENABLED=0" "${GO_EXECUTABLE}" build -trimpath ${GO_BUILD_LDFLAGS} -o "${BACKEND_OUTPUT}" .
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/go.mod" "${CMAKE_CURRENT_SOURCE_DIR}/go.sum" ${GO_SOURCES}
        COMMENT "Building SATSSDV-Go backend (audio runtime stub)"
        VERBATIM
    )
endif()
add_custom_target(satssdv-backend-build ALL DEPENDS "${BACKEND_OUTPUT}")

if(NOT SATSSDV_BUILD_BACKEND_ONLY)
    find_package(Qt6 6.4 REQUIRED COMPONENTS Widgets Network)
    set(SATSSDV_QT_SOURCES
        qt6/main.cpp
        qt6/mainwindow.cpp
        qt6/mainwindow.h
        qt6/backend_client.cpp
        qt6/backend_client.h
        qt6/template_canvas.cpp
        qt6/template_canvas.h
    )
    if(APPLE)
        add_executable(SATSSDV-Go MACOSX_BUNDLE ${SATSSDV_QT_SOURCES})
    elseif(WIN32)
        # WIN32 selects the Windows GUI subsystem (no attached console window).
        add_executable(SATSSDV-Go WIN32 ${SATSSDV_QT_SOURCES})
    else()
        add_executable(SATSSDV-Go ${SATSSDV_QT_SOURCES})
    endif()
    target_link_libraries(SATSSDV-Go PRIVATE Qt6::Widgets Qt6::Network)
    add_dependencies(SATSSDV-Go satssdv-backend-build)
    add_custom_command(TARGET SATSSDV-Go POST_BUILD
        COMMAND "${CMAKE_COMMAND}" -E copy_if_different "${BACKEND_OUTPUT}" "$<TARGET_FILE_DIR:SATSSDV-Go>/${BACKEND_NAME}"
        VERBATIM
    )

    if(WIN32)
        find_program(WINDEPLOYQT_EXECUTABLE
            NAMES windeployqt windeployqt6
            HINTS "${Qt6_DIR}/../../../bin"
            REQUIRED
        )
        add_custom_command(TARGET SATSSDV-Go POST_BUILD
            COMMAND "${WINDEPLOYQT_EXECUTABLE}" --no-translations --compiler-runtime "$<TARGET_FILE:SATSSDV-Go>"
            VERBATIM
        )
        if(MINGW)
            # Go's cgo audio backend may also depend on MinGW runtime DLLs.
            get_filename_component(MINGW_BIN_DIR "${CMAKE_CXX_COMPILER}" DIRECTORY)
            foreach(RUNTIME_DLL libgcc_s_seh-1.dll libstdc++-6.dll libwinpthread-1.dll)
                if(EXISTS "${MINGW_BIN_DIR}/${RUNTIME_DLL}")
                    add_custom_command(TARGET SATSSDV-Go POST_BUILD
                        COMMAND "${CMAKE_COMMAND}" -E copy_if_different
                            "${MINGW_BIN_DIR}/${RUNTIME_DLL}" "$<TARGET_FILE_DIR:SATSSDV-Go>/${RUNTIME_DLL}"
                        VERBATIM
                    )
                    install(FILES "${MINGW_BIN_DIR}/${RUNTIME_DLL}" DESTINATION .)
                endif()
            endforeach()
        endif()
    elseif(APPLE)
        find_program(MACDEPLOYQT_EXECUTABLE
            NAMES macdeployqt macdeployqt6
            HINTS "${Qt6_DIR}/../../../bin"
            REQUIRED
        )
        # macdeployqt must be able to resolve non-Qt Homebrew dependencies
        # (Brotli, ICU, HarfBuzz, etc.) before it rewrites their install names.
        set(MACDEPLOYQT_LIBPATH_ARGS)
        if(DEFINED SATSSDV_HOMEBREW_PREFIX AND NOT SATSSDV_HOMEBREW_PREFIX STREQUAL "")
            file(GLOB HOMEBREW_LIBRARY_DIRS LIST_DIRECTORIES true
                "${SATSSDV_HOMEBREW_PREFIX}/opt/*/lib")
            foreach(LIB_DIR IN LISTS HOMEBREW_LIBRARY_DIRS)
                list(APPEND MACDEPLOYQT_LIBPATH_ARGS "-libpath=${LIB_DIR}")
            endforeach()
            list(APPEND MACDEPLOYQT_LIBPATH_ARGS "-libpath=${SATSSDV_HOMEBREW_PREFIX}/lib")
        endif()
        add_custom_command(TARGET SATSSDV-Go POST_BUILD
            COMMAND "${MACDEPLOYQT_EXECUTABLE}"
                "$<TARGET_BUNDLE_DIR:SATSSDV-Go>"
                -codesign=-
                ${MACDEPLOYQT_LIBPATH_ARGS}
            VERBATIM
        )
    endif()

    if(APPLE)
        install(TARGETS SATSSDV-Go RUNTIME DESTINATION . BUNDLE DESTINATION .)
        # QProcess resolves the backend beside the executable inside the app bundle.
        install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION "SATSSDV-Go.app/Contents/MacOS")
    elseif(WIN32)
        install(TARGETS SATSSDV-Go RUNTIME DESTINATION .)
        install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION .)
    else()
        # Keep the Qt client and Go backend side by side for QProcess lookup.
        install(TARGETS SATSSDV-Go RUNTIME DESTINATION bin)
        install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION bin)
        install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/packaging/linux/satssdv-go.desktop"
            DESTINATION share/applications)
        install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/packaging/linux/satssdv-go.svg"
            DESTINATION share/icons/hicolor/scalable/apps)
    endif()
else()
    install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION .)
endif()

# Debian package: let dpkg-shlibdeps derive the runtime package dependencies
# from both installed ELF executables (Qt client and Go audio backend).
if(UNIX AND NOT APPLE AND NOT SATSSDV_BUILD_BACKEND_ONLY)
    set(CPACK_PACKAGE_NAME "satssdv-go")
    set(CPACK_PACKAGE_VENDOR "BH2VSQ")
    set(CPACK_PACKAGE_CONTACT "BH2VSQ")
    set(CPACK_PACKAGE_DESCRIPTION_SUMMARY "Native Qt6 satellite digital image modem")
    set(CPACK_PACKAGE_VERSION "${PROJECT_VERSION}")
    set(CPACK_PACKAGING_INSTALL_PREFIX "/usr")
    set(CPACK_GENERATOR "DEB")
    set(CPACK_DEBIAN_PACKAGE_MAINTAINER "BH2VSQ")
    set(CPACK_DEBIAN_PACKAGE_SECTION "hamradio")
    set(CPACK_DEBIAN_PACKAGE_SHLIBDEPS ON)
    set(CPACK_DEBIAN_FILE_NAME "DEB-DEFAULT")
    include(CPack)
endif()
