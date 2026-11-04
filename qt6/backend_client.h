#pragma once

#include <QJsonArray>
#include <QJsonObject>
#include <QJsonValue>
#include <QNetworkAccessManager>
#include <QProcess>
#include <QByteArray>
#include <QString>
#include <functional>

class BackendClient final {
public:
    using RpcCallback = std::function<void(bool, const QJsonValue &, const QString &)>;
    using EventsCallback = std::function<void(const QJsonArray &, quint64, bool, const QString &)>;

    BackendClient();
    ~BackendClient();

    bool start(const QString &applicationDirectory, QString *errorMessage);
    bool isRunning() const;
    void call(const QString &method, const QJsonArray &arguments, RpcCallback callback);
    void pollEvents(quint64 after, EventsCallback callback);
    QJsonValue callSync(const QString &method, const QJsonArray &arguments, QString *errorMessage = nullptr, int timeoutMs = 30000);
    void shutdown();
    QString backendAddress() const { return m_address; }

private:
    QNetworkRequest request(const QString &path) const;

    QProcess m_process;
    QNetworkAccessManager m_network;
    QString m_address;
    QString m_token;
    bool m_polling = false;
    QByteArray m_standardErrorTail;
};
