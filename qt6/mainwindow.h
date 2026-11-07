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
#include <QListWidget>
#include <QCheckBox>
#include <QSpinBox>
#include <QPushButton>
#include <QWidget>
#include <QSlider>
#include <QColor>
#include <QHash>

class SpectrumWaterfallWidget;
class ImagePreviewLabel;
class TemplateCanvas;
class QHBoxLayout;
class QGridLayout;
class QTabWidget;

class MainWindow final : public QMainWindow {
public:
    explicit MainWindow(BackendClient *backend, const QString &startupError = QString(), QWidget *parent = nullptr);
    void stopBackgroundPolling();

private:
    void buildUi();
    void loadInitialState();
    void refreshStatus();
    void refreshLatestRXPreview();
    void refreshAlbum();
    void refreshGallery();
    void refreshTemplates();
    void refreshTemplateComponentList();
    void refreshTemplateCanvas();
    void refreshInlineTemplateEditor();
    void applyInlineTemplateProperties();
    void addInlineTemplateComponent(const QString &type, const QString &assetId = QString());
    void selectInlineGalleryAsset();
    void updateRXToggle();
    void updateTXToggle();
    void updateRepeatControls();
    void updateTXProfileControls();
    void clearHisCallForCQTemplate();
    void toggleRX();
    void toggleTX();
    void applyLanguage(const QString &locale);
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
    QJsonObject buildRenderedTXPayload(const QImage &preview, QString *errorMessage = nullptr);
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
    bool m_rxActive = false;
    bool m_txActive = false;
    bool m_txToneActive = false;
    bool m_txPreparing = false;
    bool m_audioConnected = false;
    bool m_hisCallManuallyEdited = false;
    bool m_txCountdownActive = false;
    int m_repeatIntervalSeconds = 120;
    QJsonObject m_settings;
    QJsonObject m_localeDictionary;
    QJsonArray m_profiles;
    QJsonArray m_templates;
    QJsonArray m_gallery;
    QJsonArray m_album;
    QComboBox *m_rxProfile = nullptr;
    QComboBox *m_txProfile = nullptr;
    QComboBox *m_quality = nullptr;
    QComboBox *m_template = nullptr;
    QLineEdit *m_templateName = nullptr;
    QLineEdit *m_callsign = nullptr;
    QLineEdit *m_locator = nullptr;
    QLineEdit *m_hisCall = nullptr;
    QLineEdit *m_report = nullptr;
    QLineEdit *m_qth = nullptr;
    QLineEdit *m_message = nullptr;
    QWidget *m_ax25SSIDField = nullptr;
    QSpinBox *m_ax25SSID = nullptr;
    QCheckBox *m_repeat = nullptr;
    QSpinBox *m_repeatInterval = nullptr;
    QProgressBar *m_txProgress = nullptr;
    QLabel *m_txMetrics = nullptr;
    QLabel *m_audioStatus = nullptr;
    QLabel *m_rxStatus = nullptr;
    QLabel *m_rxMeta = nullptr;
    ImagePreviewLabel *m_rxPreview = nullptr;
    ImagePreviewLabel *m_txPreview = nullptr;
    TemplateCanvas *m_templateCanvas = nullptr;
    QPushButton *m_rxToggle = nullptr;
    QPushButton *m_txToggle = nullptr;
    QPushButton *m_txToneToggle = nullptr;
    QPushButton *m_workbenchNav = nullptr;
    QPushButton *m_albumNav = nullptr;
    QComboBox *m_languageSelect = nullptr;
    QLabel *m_rxSpectrumStatus = nullptr;
    QLabel *m_txSpectrumStatus = nullptr;
    SpectrumWaterfallWidget *m_rxSpectrum = nullptr;
    SpectrumWaterfallWidget *m_txSpectrum = nullptr;
    QHBoxLayout *m_rxProfileButtons = nullptr;
    QHBoxLayout *m_txProfileButtons = nullptr;
    QComboBox *m_albumFilter = nullptr;
    QWidget *m_albumCardsHost = nullptr;
    QGridLayout *m_albumCardsLayout = nullptr;
    QLabel *m_templateComponentsSummary = nullptr;
    QListWidget *m_templateComponentList = nullptr;
    QTableWidget *m_templatePropertiesTable = nullptr;
    QTextEdit *m_componentTextEdit = nullptr;
    QComboBox *m_componentAssetCombo = nullptr;
    QSlider *m_componentScaleSlider = nullptr;
    QSlider *m_componentFontSlider = nullptr;
    QSlider *m_componentOutlineSlider = nullptr;
    QPushButton *m_componentColorButton = nullptr;
    QPushButton *m_componentOutlineColorButton = nullptr;
    QCheckBox *m_componentBoldCheck = nullptr;
    QCheckBox *m_componentVisibleCheck = nullptr;
    QCheckBox *m_componentOutlineCheck = nullptr;
    QLabel *m_componentTextLabel = nullptr;
    QLabel *m_componentAssetLabel = nullptr;
    QLabel *m_componentScaleLabel = nullptr;
    QLabel *m_componentFontLabel = nullptr;
    QLabel *m_componentOutlineLabel = nullptr;
    bool m_lastRXPreviewIsGIF = false;
    QTabWidget *m_mainTabs = nullptr;
    QHash<QString, QImage> m_templateImageCache;
    QTextEdit *m_log = nullptr;
};
