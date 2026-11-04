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

file(GLOB_RECURSE GO_SOURCES CONFIGURE_DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/*.go")

if(GO_BUILD_TAGS)
    add_custom_command(
        OUTPUT "${BACKEND_OUTPUT}"
        COMMAND "${CMAKE_COMMAND}" -E make_directory "${CMAKE_RUNTIME_OUTPUT_DIRECTORY}"
        COMMAND "${CMAKE_COMMAND}" -E env "CGO_ENABLED=1" "${GO_EXECUTABLE}" build -trimpath -tags "${GO_BUILD_TAGS}" -o "${BACKEND_OUTPUT}" .
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
        COMMAND "${CMAKE_COMMAND}" -E env "CGO_ENABLED=0" "${GO_EXECUTABLE}" build -trimpath -o "${BACKEND_OUTPUT}" .
        WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
        DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/go.mod" "${CMAKE_CURRENT_SOURCE_DIR}/go.sum" ${GO_SOURCES}
        COMMENT "Building SATSSDV-Go backend (audio runtime stub)"
        VERBATIM
    )
endif()
add_custom_target(satssdv-backend-build ALL DEPENDS "${BACKEND_OUTPUT}")

if(NOT SATSSDV_BUILD_BACKEND_ONLY)
    find_package(Qt6 6.4 REQUIRED COMPONENTS Widgets Network)
    if(APPLE)
        add_executable(SATSSDV-Go MACOSX_BUNDLE
            qt6/main.cpp
            qt6/mainwindow.cpp
            qt6/mainwindow.h
            qt6/backend_client.cpp
            qt6/backend_client.h
            qt6/template_canvas.cpp
            qt6/template_canvas.h
        )
    else()
        add_executable(SATSSDV-Go
            qt6/main.cpp
            qt6/mainwindow.cpp
            qt6/mainwindow.h
            qt6/backend_client.cpp
            qt6/backend_client.h
            qt6/template_canvas.cpp
            qt6/template_canvas.h
        )
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
        add_custom_command(TARGET SATSSDV-Go POST_BUILD
            COMMAND "${MACDEPLOYQT_EXECUTABLE}" "$<TARGET_BUNDLE_DIR:SATSSDV-Go>"
            VERBATIM
        )
    endif()

    install(TARGETS SATSSDV-Go RUNTIME DESTINATION . BUNDLE DESTINATION .)
    if(APPLE)
        # QProcess resolves the backend beside the executable inside the app bundle.
        install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION "SATSSDV-Go.app/Contents/MacOS")
    else()
        # Windows and Linux executables share the same runtime directory.
        install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION .)
    endif()
else()
    install(PROGRAMS "${BACKEND_OUTPUT}" DESTINATION .)
endif()
