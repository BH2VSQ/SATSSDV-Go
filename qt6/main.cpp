#include "backend_client.h"
#include "mainwindow.h"

#include <QApplication>
#include <QMessageBox>

int main(int argc, char *argv[]) {
    QApplication application(argc, argv);
    QCoreApplication::setOrganizationName(QStringLiteral("BH2VSQ"));
    QCoreApplication::setApplicationName(QStringLiteral("SATSSDV-Go"));
    BackendClient backend;
    QString error;
    if (!backend.start(QCoreApplication::applicationDirPath(), &error)) {
        QMessageBox::critical(nullptr, QStringLiteral("SATSSDV-Go 后端启动失败"), error);
        return 2;
    }
    MainWindow window(&backend);
    window.show();
    const int result = application.exec();
    window.stopBackgroundPolling();
    backend.shutdown();
    return result;
}
