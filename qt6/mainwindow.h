#pragma once

#include "backend_client.h"

#include <QJsonArray>
#include <QJsonObject>
#include <QMainWindow>
#include <QImage>
#include <QTimer>
#include <QLabel>
#include <QComboBox>
#include <QLineEdit>
#include <QProgressBar>
#include <QTextEdit>
#include <QTableWidget>
#include <QCheckBox>
#include <QSpinBox>
#include <QPushButton>
#include <QWidget>

class SpectrumWaterfallWidget;
class ImagePreviewLabel;

class MainWindow final : public QMainWindow {
public:
    explicit MainWindow(BackendClient *backend, QWidget *parent = nullptr);
    void stopBackgroundPolling();

private:
    void buildUi();
    void loadInitialState();
    void refreshStatus();
    void refreshLatestRXPreview();
    void refreshAlbum();
    void refreshGallery();
    void refreshTemplates();
    void pollEvents();
    void handleEvent(const QJsonObject &event);
    void startRX();
    void stopRX();
    void startTX();
    void openSettings();
    void saveSettings(const QJsonObject &settings);
    void showError(const QString &message);
    void showImage(const QString &dataUrl, ImagePreviewLabel *target);
    QString selectedQuality() const;
    QImage renderTemplate(QString *errorMessage = nullptr);
    QByteArray imageDataUrlBytes(const QString &dataUrl) const;
    QImage imageFromDataUrl(const QString &dataUrl) const;
    QJsonObject selectedTemplate() const;
    QString resolveTemplateText(QString text) const;
    void appendLog(const QString &text);
    void importGalleryImage();
    void exportAlbum();
    void exportTemplatePack();
    void importTemplatePack();
    void editSelectedTemplate();
    void selectTemplateBackground();

    BackendClient *m_backend = nullptr;
    QTimer m_eventTimer;
    QTimer m_statusTimer;
    quint64 m_eventSequence = 0;
    bool m_statusRequestPending = false;
    bool m_rxPreviewRequestPending = false;
    bool m_rxPreviewRefreshQueued = false;
    QJsonObject m_settings;
    QJsonArray m_profiles;
    QJsonArray m_templates;
    QJsonArray m_gallery;
    QJsonArray m_album;
    QComboBox *m_rxProfile = nullptr;
    QComboBox *m_txProfile = nullptr;
    QComboBox *m_quality = nullptr;
    QComboBox *m_template = nullptr;
    QLineEdit *m_callsign = nullptr;
    QLineEdit *m_locator = nullptr;
    QLineEdit *m_hisCall = nullptr;
    QLineEdit *m_report = nullptr;
    QLineEdit *m_qth = nullptr;
    QLineEdit *m_message = nullptr;
    QLineEdit *m_ax25To = nullptr;
    QSpinBox *m_ax25SSID = nullptr;
    QCheckBox *m_repeat = nullptr;
    QSpinBox *m_repeatInterval = nullptr;
    QProgressBar *m_txProgress = nullptr;
    QLabel *m_audioStatus = nullptr;
    QLabel *m_rxStatus = nullptr;
    ImagePreviewLabel *m_rxPreview = nullptr;
    ImagePreviewLabel *m_txPreview = nullptr;
    QLabel *m_spectrumStatus = nullptr;
    SpectrumWaterfallWidget *m_spectrum = nullptr;
    QTextEdit *m_log = nullptr;
    QTableWidget *m_albumTable = nullptr;
};
