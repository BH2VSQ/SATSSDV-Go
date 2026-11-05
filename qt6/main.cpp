#include "backend_client.h"
#include "mainwindow.h"

#include <QApplication>

int main(int argc, char *argv[]) {
    QApplication application(argc, argv);
    QCoreApplication::setOrganizationName(QStringLiteral("BH2VSQ"));
    QCoreApplication::setApplicationName(QStringLiteral("SATSSDV-Go"));
    BackendClient backend;
    QString error;
    // Never show a startup dialog or launch a console window.  If the backend
    // cannot start, keep the single main window visible and report the reason
    // in its status area instead of creating a second top-level window.
    const bool backendStarted = backend.start(QCoreApplication::applicationDirPath(), &error);
    MainWindow window(&backend, backendStarted ? QString() : error);
    window.show();
    const int result = application.exec();
    window.stopBackgroundPolling();
    backend.shutdown();
    return result;
}
