#include "backend_client.h"

#include <QCoreApplication>
#include <QElapsedTimer>
#include <QEventLoop>
#include <QTimer>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QNetworkProxy>
#include <QRegularExpression>
#include <QNetworkRequest>
#include <QDir>
#include <QFileInfo>
#include <QUrl>
#include <utility>

BackendClient::BackendClient() {
    // This channel is deliberately loopback-only. Do not let a desktop proxy
    // redirect local RPC traffic through an external proxy server.
    m_network.setProxy(QNetworkProxy(QNetworkProxy::NoProxy));
}

BackendClient::~BackendClient() {
    shutdown();
}

bool BackendClient::start(const QString &applicationDirectory, QString *errorMessage) {
#ifdef Q_OS_WIN
    const QString executableName = QStringLiteral("satssdv-backend.exe");
#else
    const QString executableName = QStringLiteral("satssdv-backend");
#endif
    QString executable = QDir(applicationDirectory).filePath(executableName);
    if (!QFileInfo::exists(executable)) {
        executable = QDir::current().filePath(executableName);
    }
    if (!QFileInfo::exists(executable)) {
        if (errorMessage) *errorMessage = QStringLiteral("Go backend was not found next to the Qt application: %1").arg(executableName);
        return false;
    }

    m_address.clear();
    m_token.clear();
    m_standardErrorTail.clear();
    m_polling = false;
    m_process.setProgram(executable);
    m_process.setWorkingDirectory(QFileInfo(executable).absolutePath());
    m_process.setProcessChannelMode(QProcess::SeparateChannels);
    // Always drain stderr. Leaving a child-process pipe unread can eventually
    // block the Go service if a device repeatedly reports an audio/PTT error.
    QObject::connect(&m_process, &QProcess::readyReadStandardError, &m_process, [this]() {
        m_standardErrorTail += m_process.readAllStandardError();
        constexpr qsizetype maximumTail = 16 * 1024;
        if (m_standardErrorTail.size() > maximumTail)
            m_standardErrorTail.remove(0, m_standardErrorTail.size() - maximumTail);
    });
    QObject::connect(&m_process, &QProcess::finished, &m_process, [this](int, QProcess::ExitStatus) {
        m_address.clear();
        m_token.clear();
        m_polling = false;
    });
    m_process.start();
    if (!m_process.waitForStarted(8000)) {
        if (errorMessage) *errorMessage = QStringLiteral("Unable to start Go backend: %1").arg(m_process.errorString());
        return false;
    }

    QByteArray handshakeLine;
    QElapsedTimer timer;
    timer.start();
    while (timer.elapsed() < 15000) {
        if (m_process.canReadLine()) {
            handshakeLine = m_process.readLine();
            break;
        }
        if (m_process.state() == QProcess::NotRunning) {
            if (errorMessage) *errorMessage = QStringLiteral("Go backend exited before publishing its local RPC endpoint: %1").arg(QString::fromUtf8(m_standardErrorTail).trimmed());
            return false;
        }
        m_process.waitForReadyRead(250);
    }
    if (handshakeLine.isEmpty()) {
        if (errorMessage) *errorMessage = QStringLiteral("Timed out waiting for the Go backend handshake.");
        shutdown();
        return false;
    }

    QJsonParseError parseError;
    const auto document = QJsonDocument::fromJson(handshakeLine, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        if (errorMessage) *errorMessage = QStringLiteral("Invalid Go backend handshake: %1").arg(parseError.errorString());
        shutdown();
        return false;
    }
    const QJsonObject handshake = document.object();
    m_address = handshake.value(QStringLiteral("address")).toString();
    m_token = handshake.value(QStringLiteral("token")).toString();
    const QUrl endpoint(m_address);
    const qint64 reportedPid = handshake.value(QStringLiteral("pid")).toVariant().toLongLong();
    const bool validEndpoint = endpoint.scheme() == QStringLiteral("http") &&
                               endpoint.host() == QStringLiteral("127.0.0.1") &&
                               endpoint.port(-1) > 0 && endpoint.path().isEmpty();
    const bool validToken = QRegularExpression(QStringLiteral("^[0-9a-f]{64}$")).match(m_token).hasMatch();
    if (!validEndpoint || !validToken || reportedPid != m_process.processId()) {
        if (errorMessage) *errorMessage = QStringLiteral("Go backend returned an invalid loopback RPC handshake.");
        shutdown();
        return false;
    }
    return true;
}

bool BackendClient::isRunning() const {
    return m_process.state() == QProcess::Running && !m_address.isEmpty();
}

QNetworkRequest BackendClient::request(const QString &path) const {
    QNetworkRequest request(QUrl(m_address + path));
    request.setRawHeader("X-SATSSDV-Token", m_token.toUtf8());
    return request;
}

void BackendClient::call(const QString &method, const QJsonArray &arguments, RpcCallback callback) {
    if (!isRunning()) {
        if (callback) callback(false, QJsonValue(), QStringLiteral("Go backend is not running."));
        return;
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("method"), method);
    payload.insert(QStringLiteral("args"), arguments);
    QNetworkRequest req = request(QStringLiteral("/rpc"));
    req.setHeader(QNetworkRequest::ContentTypeHeader, QStringLiteral("application/json"));
    QNetworkReply *reply = m_network.post(req, QJsonDocument(payload).toJson(QJsonDocument::Compact));
    reply->setProperty("rpcTimedOut", false);
    auto *timeout = new QTimer(reply);
    timeout->setSingleShot(true);
    QObject::connect(timeout, &QTimer::timeout, reply, [reply]() {
        if (!reply->isFinished()) {
            reply->setProperty("rpcTimedOut", true);
            reply->abort();
        }
    });
    timeout->start(30000);
    QObject::connect(reply, &QNetworkReply::finished, reply, [reply, timeout, callback = std::move(callback)]() {
        timeout->stop();
        const QByteArray bytes = reply->readAll();
        const auto networkError = reply->error();
        const QString networkMessage = reply->property("rpcTimedOut").toBool()
            ? QStringLiteral("Backend RPC timed out.") : reply->errorString();
        reply->deleteLater();
        if (networkError != QNetworkReply::NoError) {
            if (callback) callback(false, QJsonValue(), networkMessage);
            return;
        }
        QJsonParseError parseError;
        const auto document = QJsonDocument::fromJson(bytes, &parseError);
        if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
            if (callback) callback(false, QJsonValue(), QStringLiteral("Invalid RPC response: %1").arg(parseError.errorString()));
            return;
        }
        const QJsonObject response = document.object();
        if (!response.value(QStringLiteral("ok")).toBool()) {
            if (callback) callback(false, QJsonValue(), response.value(QStringLiteral("error")).toString(QStringLiteral("Backend operation failed.")));
            return;
        }
        if (callback) callback(true, response.value(QStringLiteral("data")), QString());
    });
}

void BackendClient::pollEvents(quint64 after, EventsCallback callback) {
    if (!isRunning() || m_polling) return;
    m_polling = true;
    QNetworkReply *reply = m_network.get(request(QStringLiteral("/events?after=%1").arg(after)));
    reply->setProperty("eventPollTimedOut", false);
    auto *timeout = new QTimer(reply);
    timeout->setSingleShot(true);
    QObject::connect(timeout, &QTimer::timeout, reply, [reply]() {
        if (!reply->isFinished()) {
            reply->setProperty("eventPollTimedOut", true);
            reply->abort();
        }
    });
    timeout->start(5000);
    QObject::connect(reply, &QNetworkReply::finished, reply, [this, reply, timeout, callback = std::move(callback)]() {
        timeout->stop();
        const QByteArray bytes = reply->readAll();
        const auto networkError = reply->error();
        const QString networkMessage = reply->property("eventPollTimedOut").toBool()
            ? QStringLiteral("Backend event poll timed out.") : reply->errorString();
        reply->deleteLater();
        m_polling = false;
        if (networkError != QNetworkReply::NoError) {
            if (callback) callback(QJsonArray(), 0, false, networkMessage);
            return;
        }
        QJsonParseError parseError;
        const auto document = QJsonDocument::fromJson(bytes, &parseError);
        if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
            if (callback) callback(QJsonArray(), 0, false, QStringLiteral("Invalid event response: %1").arg(parseError.errorString()));
            return;
        }
        const auto object = document.object();
        if (callback) callback(object.value(QStringLiteral("events")).toArray(),
                               static_cast<quint64>(object.value(QStringLiteral("next")).toVariant().toULongLong()),
                               object.value(QStringLiteral("reset")).toBool(), QString());
    });
}


QJsonValue BackendClient::callSync(const QString &method, const QJsonArray &arguments, QString *errorMessage, int timeoutMs) {
    if (!isRunning()) {
        if (errorMessage) *errorMessage = QStringLiteral("Go backend is not running.");
        return {};
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("method"), method);
    payload.insert(QStringLiteral("args"), arguments);
    QNetworkRequest req = request(QStringLiteral("/rpc"));
    req.setHeader(QNetworkRequest::ContentTypeHeader, QStringLiteral("application/json"));

    QNetworkReply *reply = m_network.post(req, QJsonDocument(payload).toJson(QJsonDocument::Compact));
    QEventLoop loop;
    QTimer timeout;
    timeout.setSingleShot(true);
    bool timedOut = false;
    QObject::connect(reply, &QNetworkReply::finished, &loop, &QEventLoop::quit);
    QObject::connect(&timeout, &QTimer::timeout, &loop, [&]() {
        timedOut = true;
        reply->abort();
        loop.quit();
    });
    timeout.start(qMax(1, timeoutMs));
    if (!reply->isFinished()) loop.exec();
    timeout.stop();

    const QByteArray bytes = reply->readAll();
    const auto networkError = reply->error();
    const QString networkMessage = reply->errorString();
    const int httpStatus = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
    reply->deleteLater();

    if (timedOut) {
        if (errorMessage) *errorMessage = QStringLiteral("Backend RPC timed out: %1").arg(method);
        return {};
    }
    if (networkError != QNetworkReply::NoError) {
        if (errorMessage) *errorMessage = networkMessage;
        return {};
    }
    if (httpStatus < 200 || httpStatus >= 300) {
        if (errorMessage) *errorMessage = QStringLiteral("Backend RPC returned HTTP %1").arg(httpStatus);
        return {};
    }

    QJsonParseError parseError;
    const auto document = QJsonDocument::fromJson(bytes, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        if (errorMessage) *errorMessage = QStringLiteral("Invalid RPC response: %1").arg(parseError.errorString());
        return {};
    }
    const QJsonObject response = document.object();
    if (!response.value(QStringLiteral("ok")).toBool()) {
        if (errorMessage) *errorMessage = response.value(QStringLiteral("error")).toString(QStringLiteral("Backend operation failed."));
        return {};
    }
    if (errorMessage) errorMessage->clear();
    return response.value(QStringLiteral("data"));
}

void BackendClient::shutdown() {
    if (m_process.state() == QProcess::NotRunning) {
        m_address.clear();
        m_token.clear();
        m_polling = false;
        return;
    }
    if (!m_address.isEmpty()) {
        QNetworkRequest req = request(QStringLiteral("/shutdown"));
        req.setHeader(QNetworkRequest::ContentTypeHeader, QStringLiteral("application/json"));
        QEventLoop loop;
        QTimer timer;
        timer.setSingleShot(true);
        QNetworkReply *reply = m_network.post(req, QByteArrayLiteral("{}"));
        QObject::connect(reply, &QNetworkReply::finished, &loop, &QEventLoop::quit);
        QObject::connect(&timer, &QTimer::timeout, &loop, &QEventLoop::quit);
        timer.start(700);
        loop.exec();
        if (!reply->isFinished()) reply->abort();
        reply->deleteLater();
    }
    if (!m_process.waitForFinished(2500)) {
        m_process.terminate();
        if (!m_process.waitForFinished(1500)) {
            m_process.kill();
            m_process.waitForFinished(1000);
        }
    }
    m_address.clear();
    m_token.clear();
}
