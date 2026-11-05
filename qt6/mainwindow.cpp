#include "mainwindow.h"
#include "template_canvas.h"

#include <QAction>
#include <QStringList>
#include <QRectF>
#include <QPen>
#include <QFont>
#include <QApplication>
#include <QDateTime>
#include <QDialog>
#include <QDialogButtonBox>
#include <QFileDialog>
#include <QFormLayout>
#include <QGroupBox>
#include <QGridLayout>
#include <QFrame>
#include <QIcon>
#include <QPointer>
#include <QHeaderView>
#include <QHBoxLayout>
#include <QJsonDocument>
#include <QJsonParseError>
#include <QMenuBar>
#include <QMenu>
#include <QCursor>
#include <QMessageBox>
#include <QPainter>
#include <QPainterPath>
#include <QScrollArea>
#include <QSettings>
#include <QStatusBar>
#include <QTabWidget>
#include <QVBoxLayout>
#include <QSplitter>
#include <QStandardPaths>
#include <QFileInfo>
#include <QFile>
#include <QBuffer>
#include <QFontDatabase>
#include <QDate>
#include <QTime>
#include <QPixmap>
#include <QAbstractItemView>
#include <QVector>
#include <QColor>
#include <QMouseEvent>
#include <QToolButton>
#include <QInputDialog>
#include <QContextMenuEvent>
#include <QNetworkReply>
#include <algorithm>
#include <cmath>
#include <cstring>
#include <QSizePolicy>
#include <QResizeEvent>
#include <QSignalBlocker>
#include <QUuid>
#include <QHash>
#include <QSet>


class ImagePreviewLabel final : public QLabel {
public:
    explicit ImagePreviewLabel(QWidget *parent = nullptr) : QLabel(parent) {
        setMinimumSize(320, 240);
        setAlignment(Qt::AlignCenter);
        setText(QStringLiteral("等待图像"));
        setStyleSheet(QStringLiteral("QLabel { background:#101b25; color:#91a7b6; border:1px solid #344553; border-radius:8px; }"));
        setScaledContents(false);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    void setSourceImage(const QImage &image) {
        m_source = image;
        updatePixmap();
    }

protected:
    void resizeEvent(QResizeEvent *event) override {
        QLabel::resizeEvent(event);
        updatePixmap();
    }

private:
    void updatePixmap() {
        if (m_source.isNull() || contentsRect().isEmpty()) return;
        setPixmap(QPixmap::fromImage(m_source.scaled(contentsRect().size(), Qt::KeepAspectRatio, Qt::SmoothTransformation)));
    }

    QImage m_source;
};

class SpectrumWaterfallWidget final : public QWidget {
public:
    explicit SpectrumWaterfallWidget(QWidget *parent = nullptr) : QWidget(parent), m_waterfall(640, 220, QImage::Format_RGB32) {
        m_waterfall.fill(QColor(QStringLiteral("#07111b")));
        setMinimumHeight(250);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    void pushBins(const QJsonArray &bins) {
        if (bins.isEmpty()) return;
        QVector<double> values;
        values.reserve(bins.size());
        for (const auto &value : bins) values.push_back(value.toObject().value(QStringLiteral("magnitudeDbfs")).toDouble(-120.0));
        if (values.isEmpty()) return;
        m_latest = values;
        if (m_waterfall.width() != 640 || m_waterfall.height() != 220) m_waterfall = QImage(640, 220, QImage::Format_RGB32);
        for (int y = 0; y < m_waterfall.height() - 1; ++y) {
            std::memcpy(m_waterfall.scanLine(y), m_waterfall.constScanLine(y + 1), size_t(m_waterfall.width()) * 4);
        }
        QRgb *row = reinterpret_cast<QRgb *>(m_waterfall.scanLine(m_waterfall.height() - 1));
        for (int x = 0; x < m_waterfall.width(); ++x) {
            const int index = qBound(0, int((qreal(x) / qMax(1, int(m_waterfall.width()) - 1)) * (values.size() - 1)), values.size() - 1);
            const double level = qBound(0.0, (values[index] + 110.0) / 90.0, 1.0);
            row[x] = colorFor(level).rgb();
        }
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override {
        QPainter painter(this);
        painter.fillRect(rect(), QColor(QStringLiteral("#07111b")));
        const QRectF waterfallRect(0, height() * 0.30, width(), height() * 0.70);
        painter.drawImage(waterfallRect, m_waterfall);
        painter.setPen(QPen(QColor(QStringLiteral("#294252")), 1));
        for (int i = 1; i < 5; ++i) {
            const qreal x = width() * i / 5.0;
            painter.drawLine(QPointF(x, 0), QPointF(x, height()));
        }
        for (int i = 1; i < 4; ++i) {
            const qreal y = height() * (0.30 + 0.70 * i / 4.0);
            painter.drawLine(QPointF(0, y), QPointF(width(), y));
        }
        if (m_latest.size() > 1) {
            QPainterPath path;
            for (int i = 0; i < m_latest.size(); ++i) {
                const qreal x = width() * qreal(i) / qMax(1, int(m_latest.size()) - 1);
                const qreal level = qBound(0.0, (m_latest[i] + 110.0) / 90.0, 1.0);
                const qreal y = height() * 0.27 - level * height() * 0.24;
                if (i == 0) path.moveTo(x, y); else path.lineTo(x, y);
            }
            painter.setPen(QPen(QColor(QStringLiteral("#65d8e8")), 1.6));
            painter.drawPath(path);
        }
        painter.setPen(QColor(QStringLiteral("#9fb3c0")));
        painter.drawText(QRect(8, 5, 160, 20), Qt::AlignLeft | Qt::AlignVCenter, QStringLiteral("SPECTRUM / WATERFALL"));
        painter.drawText(QRect(width() - 100, 5, 92, 20), Qt::AlignRight | Qt::AlignVCenter, QStringLiteral("0–24 kHz"));
    }

private:
    static QColor colorFor(double value) {
        const QColor a(QStringLiteral("#07111b"));
        const QColor b(QStringLiteral("#153a78"));
        const QColor c(QStringLiteral("#168fae"));
        const QColor d(QStringLiteral("#39c6b0"));
        const QColor e(QStringLiteral("#f7d45b"));
        if (value < 0.30) return QColor::fromRgbF(a.redF() + (b.redF()-a.redF())*value/0.30, a.greenF() + (b.greenF()-a.greenF())*value/0.30, a.blueF() + (b.blueF()-a.blueF())*value/0.30);
        if (value < 0.58) { const double t=(value-0.30)/0.28; return QColor::fromRgbF(b.redF()+(c.redF()-b.redF())*t,b.greenF()+(c.greenF()-b.greenF())*t,b.blueF()+(c.blueF()-b.blueF())*t); }
        if (value < 0.84) { const double t=(value-0.58)/0.26; return QColor::fromRgbF(c.redF()+(d.redF()-c.redF())*t,c.greenF()+(d.greenF()-c.greenF())*t,c.blueF()+(d.blueF()-c.blueF())*t); }
        const double t=(value-0.84)/0.16; return QColor::fromRgbF(d.redF()+(e.redF()-d.redF())*t,d.greenF()+(e.greenF()-d.greenF())*t,d.blueF()+(e.blueF()-d.blueF())*t);
    }
    QImage m_waterfall;
    QVector<double> m_latest;
};

namespace {
QComboBox *makeCombo(QWidget *parent, const QStringList &items) {
    auto *combo = new QComboBox(parent);
    combo->addItems(items);
    combo->setMinimumHeight(30);
    return combo;
}

ImagePreviewLabel *makePreview(QWidget *parent) {
    return new ImagePreviewLabel(parent);
}

QString prettyBytes(const QByteArray &data) {
    return QString::number(data.size()) + QStringLiteral(" bytes");
}

QString valueOr(const QJsonObject &object, const QString &key, const QString &fallback = QString()) {
    const QString value = object.value(key).toString();
    return value.isEmpty() ? fallback : value;
}
}

MainWindow::MainWindow(BackendClient *backend, const QString &startupError, QWidget *parent)
    : QMainWindow(parent), m_backend(backend) {
    buildUi();
    if (m_backend && m_backend->isRunning()) {
        loadInitialState();
        connect(&m_eventTimer, &QTimer::timeout, this, [this] { pollEvents(); });
        connect(&m_statusTimer, &QTimer::timeout, this, [this] { refreshStatus(); });
        m_eventTimer.start(80);
        m_statusTimer.start(2000);
        refreshStatus();
    } else {
        // Startup failures must not create a second top-level window.  The
        // operator always gets the main window and an inline diagnostic.
        m_audioStatus->setText(QStringLiteral("后端启动失败"));
        m_audioStatus->setToolTip(startupError);
        m_audioStatus->setStyleSheet(QStringLiteral("background:#f8eeee; border:1px solid #d7b2b7; padding:6px 10px; color:#98545c;"));
        statusBar()->showMessage(startupError.isEmpty() ? QStringLiteral("Go 后端不可用") : QStringLiteral("Go 后端启动失败：%1").arg(startupError));
        appendLog(startupError);
    }
}

void MainWindow::stopBackgroundPolling() {
    m_eventTimer.stop();
    m_statusTimer.stop();
}

void MainWindow::buildUi() {
    setWindowTitle(QStringLiteral("SATSSDV-Go"));
    setMinimumSize(1180, 760);
    resize(1600, 980);

    setStyleSheet(QStringLiteral(
        "QMainWindow,QWidget { background:#e9ecef; color:#35424b; font-family:'Segoe UI','Microsoft YaHei UI','Noto Sans CJK SC'; font-size:9pt; }"
        "QTabWidget::pane { border:1px solid #aeb7be; background:#e9ecef; }"
        "QTabBar::tab { background:#e5e8ea; color:#58636d; min-width:86px; padding:9px 18px; margin-right:3px; border:1px solid #c1c8cd; border-bottom:0; }"
        "QTabBar::tab:selected { background:#f7f9fa; color:#2b596e; font-weight:700; border-bottom:2px solid #5a9db8; }"
        "QGroupBox { border:1px solid #aeb7be; margin-top:10px; padding:8px; background:#f2f4f5; }"
        "QGroupBox::title { subcontrol-origin:margin; left:8px; padding:0 4px; color:#4c6371; font-weight:700; }"
        "QPushButton { background:linear-gradient(#ffffff,#e7e9eb); border:1px solid #b5bec5; border-radius:3px; padding:5px 10px; color:#41515d; min-height:20px; }"
        "QPushButton:hover { background:#edf6fa; border-color:#83aabd; }"
        "QPushButton:checked { background:linear-gradient(#d9eff7,#b9dce9); border-color:#75a7bb; color:#204e64; font-weight:700; }"
        "QPushButton:disabled { color:#929ba2; background:#e4e7e9; }"
        "QPushButton[role='primary'] { background:linear-gradient(#e1f2f8,#c2e0eb); border-color:#83a9ba; color:#28576b; font-weight:700; }"
        "QPushButton[role='danger'] { background:linear-gradient(#faeeee,#f0d8db); border-color:#d1a4aa; color:#98545c; }"
        "QComboBox,QLineEdit,QSpinBox,QTextEdit { background:#ffffff; border:1px solid #b9c1c7; border-radius:2px; padding:4px 6px; color:#37434c; selection-background-color:#b9dce9; }"
        "QComboBox,QLineEdit,QSpinBox { min-height:22px; }"
        "QHeaderView::section { background:#e2e6e9; color:#4b5b66; padding:6px; border:0; border-bottom:1px solid #c4cbd0; }"
        "QTableWidget { background:#ffffff; alternate-background-color:#f3f5f6; gridline-color:#d6dce0; border:1px solid #c4cbd0; }"
        "QProgressBar { border:1px solid #c4cbd0; background:#dfe3e6; height:7px; text-align:center; }"
        "QProgressBar::chunk { background:#6ba7bc; }"
        "QScrollArea { border:0; background:#e7eaed; }"
        "QStatusBar { background:#f4f5f6; color:#71808a; border-top:1px solid #c3cbd1; }"
    ));

    auto *central = new QWidget(this);
    auto *root = new QVBoxLayout(central);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    // Compact application header matching the original desktop layout.
    auto *header = new QWidget(central);
    header->setFixedHeight(58);
    header->setStyleSheet(QStringLiteral("background:linear-gradient(#ffffff,#e7e9eb); border-bottom:1px solid #aeb6bd;"));
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(15, 5, 14, 5);
    headerLayout->setSpacing(12);
    auto *mark = new QLabel(QStringLiteral("SD"), header);
    mark->setAlignment(Qt::AlignCenter);
    mark->setFixedSize(32, 32);
    mark->setStyleSheet(QStringLiteral("background:linear-gradient(145deg,#cceefa,#f5dce5); color:#1c4558; border:1px solid #7995a3; border-radius:4px; font-weight:900;"));
    auto *brand = new QLabel(QStringLiteral("SATSSDV-Go\nSatellite Digital Image Workstation"), header);
    brand->setStyleSheet(QStringLiteral("font-weight:700; color:#2f4653; font-size:10pt;"));
    QFont brandFont = brand->font(); brandFont.setBold(true); brand->setFont(brandFont);
    headerLayout->addWidget(mark);
    headerLayout->addWidget(brand);
    headerLayout->addStretch();
    m_audioStatus = new QLabel(QStringLiteral("音频：正在连接"), header);
    m_audioStatus->setMinimumWidth(205);
    m_audioStatus->setAlignment(Qt::AlignCenter);
    m_audioStatus->setStyleSheet(QStringLiteral("background:#f3f5f6; border:1px solid #c4cbd0; padding:6px 10px; color:#6b747c;"));
    auto *settingsButton = new QPushButton(QStringLiteral("⚙"), header);
    settingsButton->setToolTip(QStringLiteral("设备设置"));
    settingsButton->setFixedSize(34, 30);
    connect(settingsButton, &QPushButton::clicked, this, [this] { openSettings(); });
    headerLayout->addWidget(m_audioStatus);
    headerLayout->addWidget(settingsButton);
    root->addWidget(header);

    // Keep the profile selectors as hidden data-backed controls so the existing
    // RX/TX commands and settings serialization continue to use the same IDs.
    m_rxProfile = makeCombo(central, {});
    m_txProfile = makeCombo(central, {});
    m_rxProfile->hide();
    m_txProfile->hide();

    auto *tabs = new QTabWidget(central);
    tabs->setDocumentMode(true);
    tabs->setMovable(false);

    auto *workbench = new QWidget(tabs);
    auto *workLayout = new QVBoxLayout(workbench);
    workLayout->setContentsMargins(6, 6, 6, 6);
    workLayout->setSpacing(5);

    auto *profilePanel = new QWidget(workbench);
    profilePanel->setStyleSheet(QStringLiteral("background:#f1f3f4; border-bottom:1px solid #abb3ba;"));
    auto *profileLayout = new QVBoxLayout(profilePanel);
    profileLayout->setContentsMargins(7, 5, 7, 5);
    profileLayout->setSpacing(4);
    auto addProfileRow = [&](const QString &title, const QString &subtitle, QHBoxLayout **target) {
        auto *row = new QWidget(profilePanel);
        auto *layout = new QHBoxLayout(row);
        layout->setContentsMargins(0, 0, 0, 0);
        layout->setSpacing(5);
        auto *label = new QLabel(title + QStringLiteral("\n") + subtitle, row);
        label->setFixedWidth(112);
        label->setStyleSheet(QStringLiteral("font-size:8pt; color:#52636f; font-weight:700;"));
        layout->addWidget(label);
        auto *buttons = new QHBoxLayout();
        buttons->setContentsMargins(0, 0, 0, 0);
        buttons->setSpacing(5);
        layout->addLayout(buttons, 1);
        auto *meta = new QLabel(title.startsWith(QStringLiteral("RX")) ? QStringLiteral("RX 待机") : QStringLiteral("TX 就绪"), row);
        meta->setMinimumWidth(100);
        meta->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        meta->setStyleSheet(QStringLiteral("font-size:8pt; color:#838c93;"));
        layout->addWidget(meta);
        profileLayout->addWidget(row);
        *target = buttons;
    };
    addProfileRow(QStringLiteral("RX PROFILE"), QStringLiteral("接收 RX"), &m_rxProfileButtons);
    addProfileRow(QStringLiteral("TX PROFILE"), QStringLiteral("发送 TX"), &m_txProfileButtons);
    workLayout->addWidget(profilePanel);

    auto *split = new QSplitter(Qt::Horizontal, workbench);
    split->setChildrenCollapsible(false);
    split->setHandleWidth(5);

    // RX workstation: image reconstruction occupies the main area; its own
    // receive spectrum/waterfall remains at the bottom of the RX column.
    auto *rxPane = new QWidget(split);
    rxPane->setStyleSheet(QStringLiteral("background:#f2f4f5; border:1px solid #929da5;"));
    auto *rxLayout = new QVBoxLayout(rxPane);
    rxLayout->setContentsMargins(8, 7, 8, 7);
    rxLayout->setSpacing(5);
    auto *rxHeader = new QHBoxLayout();
    auto *rxTitle = new QLabel(QStringLiteral("RECEIVE\n接收 RX"), rxPane);
    rxTitle->setStyleSheet(QStringLiteral("font-size:10pt; font-weight:700; color:#314b5a;"));
    m_rxStatus = new QLabel(QStringLiteral("RX 待机 · 等待接收音频"), rxPane);
    m_rxStatus->setStyleSheet(QStringLiteral("font-size:8pt; color:#77838b;"));
    m_rxStatus->setWordWrap(true);
    auto *rxStart = new QPushButton(QStringLiteral("启动 RX"), rxPane);
    rxStart->setProperty("role", "primary");
    auto *rxStop = new QPushButton(QStringLiteral("停止 RX"), rxPane);
    rxStop->setProperty("role", "danger");
    connect(rxStart, &QPushButton::clicked, this, [this] { startRX(); });
    connect(rxStop, &QPushButton::clicked, this, [this] { stopRX(); });
    rxHeader->addWidget(rxTitle);
    rxHeader->addStretch();
    rxHeader->addWidget(m_rxStatus, 1);
    rxHeader->addWidget(rxStart);
    rxHeader->addWidget(rxStop);
    rxLayout->addLayout(rxHeader);
    m_rxPreview = makePreview(rxPane);
    m_rxPreview->setMinimumSize(340, 260);
    m_rxPreview->setStyleSheet(QStringLiteral("QLabel { background:#ffffff; color:#87939b; border:1px solid #a1aab1; border-radius:0; }"));
    rxLayout->addWidget(m_rxPreview, 5);
    m_rxMeta = new QLabel(QStringLiteral("CALL  ---      IMAGE ID  --      PACKETS  0 / 0      MODE  ---"), rxPane);
    m_rxMeta->setStyleSheet(QStringLiteral("font-size:8pt; color:#7e8991; padding:2px 1px;"));
    rxLayout->addWidget(m_rxMeta);
    auto *rxSpectrumTitle = new QHBoxLayout();
    rxSpectrumTitle->addWidget(new QLabel(QStringLiteral("RX 频谱 / 瀑布图"), rxPane));
    rxSpectrumTitle->addStretch();
    auto *rxSpectrumHint = new QLabel(QStringLiteral("输入 · 音频瀑布图"), rxPane);
    rxSpectrumHint->setStyleSheet(QStringLiteral("font-size:8pt; color:#919aa1;"));
    rxSpectrumTitle->addWidget(rxSpectrumHint);
    rxLayout->addLayout(rxSpectrumTitle);
    m_rxSpectrumStatus = new QLabel(QStringLiteral("等待 RX 音频"), rxPane);
    m_rxSpectrumStatus->setStyleSheet(QStringLiteral("font-size:8pt; color:#77858f;"));
    rxLayout->addWidget(m_rxSpectrumStatus);
    m_rxSpectrum = new SpectrumWaterfallWidget(rxPane);
    m_rxSpectrum->setMinimumHeight(95);
    m_rxSpectrum->setMaximumHeight(155);
    m_rxSpectrum->setStyleSheet(QStringLiteral("border:1px solid #7e8c96;"));
    rxLayout->addWidget(m_rxSpectrum, 1);
    auto *rxFooter = new QLabel(QStringLiteral("LEVEL  -∞ dBFS       PEAK  ---       横轴：频率 · 纵轴：时间"), rxPane);
    rxFooter->setStyleSheet(QStringLiteral("font-size:8pt; color:#859099; padding:2px;"));
    rxLayout->addWidget(rxFooter);

    // TX workstation: all common transmission controls remain in one compact
    // strip, with the image canvas/QSO fields and template tools side-by-side.
    auto *txPane = new QWidget(split);
    txPane->setStyleSheet(QStringLiteral("background:#f2f4f5; border:1px solid #929da5;"));
    auto *txLayout = new QVBoxLayout(txPane);
    txLayout->setContentsMargins(8, 7, 8, 7);
    txLayout->setSpacing(5);
    auto *txHeader = new QHBoxLayout();
    auto *txTitle = new QLabel(QStringLiteral("TRANSMIT\n发送 TX"), txPane);
    txTitle->setStyleSheet(QStringLiteral("font-size:10pt; font-weight:700; color:#314b5a;"));
    txHeader->addWidget(txTitle);
    txHeader->addStretch();
    txHeader->addWidget(new QLabel(QStringLiteral("图片质量"), txPane));
    m_quality = makeCombo(txPane, {QStringLiteral("低质量 320×240"), QStringLiteral("中质量 448×336"), QStringLiteral("高质量 640×480")});
    m_quality->setCurrentIndex(1);
    m_quality->setMinimumWidth(125);
    txHeader->addWidget(m_quality);
    m_repeat = new QCheckBox(QStringLiteral("循环发送"), txPane);
    txHeader->addWidget(m_repeat);
    m_repeatInterval = new QSpinBox(txPane);
    m_repeatInterval->setRange(0, 86400);
    m_repeatInterval->setValue(120);
    m_repeatInterval->setSuffix(QStringLiteral(" 秒"));
    m_repeatInterval->setMaximumWidth(90);
    txHeader->addWidget(m_repeatInterval);
    auto *send = new QPushButton(QStringLiteral("开始发射"), txPane);
    send->setProperty("role", "primary");
    auto *tone = new QPushButton(QStringLiteral("1500 Hz 测试音"), txPane);
    auto *stop = new QPushButton(QStringLiteral("停止 TX"), txPane);
    stop->setProperty("role", "danger");
    txHeader->addWidget(send);
    txHeader->addWidget(tone);
    txHeader->addWidget(stop);
    txLayout->addLayout(txHeader);
    connect(send, &QPushButton::clicked, this, [this] { startTX(); });
    connect(tone, &QPushButton::clicked, this, [this] {
        m_backend->call(QStringLiteral("Start1500HzTestTone"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
            if (ok) appendLog(QStringLiteral("已启动 1500 Hz 测试音")); else showError(error);
        });
    });
    connect(stop, &QPushButton::clicked, this, [this] {
        m_backend->call(QStringLiteral("StopTX"), {}, [this](bool, const QJsonValue &, const QString &) { appendLog(QStringLiteral("已请求停止发射")); });
    });

    auto *templateStrip = new QWidget(txPane);
    auto *templateStripLayout = new QGridLayout(templateStrip);
    templateStripLayout->setContentsMargins(0, 2, 0, 5);
    templateStripLayout->setHorizontalSpacing(6);
    templateStripLayout->setVerticalSpacing(4);
    templateStripLayout->addWidget(new QLabel(QStringLiteral("预设名称"), templateStrip), 0, 0);
    m_templateName = new QLineEdit(templateStrip);
    m_templateName->setMaxLength(48);
    templateStripLayout->addWidget(m_templateName, 0, 1);
    auto *savePreset = new QPushButton(QStringLiteral("保存预设"), templateStrip);
    templateStripLayout->addWidget(savePreset, 0, 2);

    templateStripLayout->addWidget(new QLabel(QStringLiteral("发射模板"), templateStrip), 1, 0);
    m_template = makeCombo(templateStrip, {});
    templateStripLayout->addWidget(m_template, 1, 1);
    auto *newPreset = new QPushButton(QStringLiteral("＋ 新建预设"), templateStrip);
    auto *duplicatePreset = new QPushButton(QStringLiteral("复制当前"), templateStrip);
    auto *deletePreset = new QPushButton(QStringLiteral("删除当前"), templateStrip);
    auto *editTemplate = new QPushButton(QStringLiteral("编辑组件…"), templateStrip);
    auto *background = new QPushButton(QStringLiteral("背景图"), templateStrip);
    auto *pack = new QPushButton(QStringLiteral("导入 / 导出模板包"), templateStrip);
    deletePreset->setProperty("role", "danger");
    templateStripLayout->addWidget(newPreset, 1, 2);
    templateStripLayout->addWidget(duplicatePreset, 1, 3);
    templateStripLayout->addWidget(deletePreset, 1, 4);
    templateStripLayout->addWidget(editTemplate, 1, 5);
    templateStripLayout->addWidget(background, 1, 6);
    templateStripLayout->addWidget(pack, 1, 7);
    templateStripLayout->setColumnStretch(1, 1);
    connect(editTemplate, &QPushButton::clicked, this, [this] { editSelectedTemplate(); });
    connect(background, &QPushButton::clicked, this, [this] { selectTemplateBackground(); });
    connect(pack, &QPushButton::clicked, this, [this] {
        QMenu menu(this);
        QAction *importAction = menu.addAction(QStringLiteral("导入模板包"));
        QAction *exportAction = menu.addAction(QStringLiteral("导出模板包"));
        QAction *chosen = menu.exec(QCursor::pos());
        if (chosen == importAction) importTemplatePack();
        else if (chosen == exportAction) exportTemplatePack();
    });
    connect(m_template, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) {
        const QJsonObject selected = selectedTemplate();
        if (m_templateName) m_templateName->setText(selected.value(QStringLiteral("name")).toString());
        refreshTemplateComponentList();
        QString error;
        const QImage image = renderTemplate(&error);
        if (!image.isNull()) m_txPreview->setSourceImage(image);
        else if (!error.isEmpty()) appendLog(error);
    });
    connect(savePreset, &QPushButton::clicked, this, [this] {
        const QString id = m_template->currentData().toString();
        const QString name = m_templateName ? m_templateName->text().trimmed() : QString();
        if (id.isEmpty() || name.isEmpty()) {
            statusBar()->showMessage(QStringLiteral("预设名称不能为空"), 5000);
            return;
        }
        QJsonArray updated;
        for (QJsonValue value : m_templates) {
            QJsonObject item = value.toObject();
            if (item.value(QStringLiteral("id")).toString() == id) item.insert(QStringLiteral("name"), name);
            updated.append(item);
        }
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this, id](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            const int index = m_template->findData(id);
            if (index >= 0) m_template->setCurrentIndex(index);
            statusBar()->showMessage(QStringLiteral("模板预设已保存"), 4000);
        });
    });
    connect(newPreset, &QPushButton::clicked, this, [this] {
        if (m_templates.size() >= 32) { statusBar()->showMessage(QStringLiteral("最多支持 32 个模板预设"), 5000); return; }
        QJsonObject item;
        const QString id = QUuid::createUuid().toString(QUuid::WithoutBraces);
        item.insert(QStringLiteral("id"), id);
        item.insert(QStringLiteral("name"), QStringLiteral("新建预设"));
        item.insert(QStringLiteral("components"), QJsonArray{});
        item.insert(QStringLiteral("updatedAt"), QDateTime::currentDateTimeUtc().toString(Qt::ISODate));
        QJsonArray updated = m_templates;
        updated.append(item);
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this, id](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            const int index = m_template->findData(id);
            if (index >= 0) m_template->setCurrentIndex(index);
            m_templateName->setText(QStringLiteral("新建预设"));
            statusBar()->showMessage(QStringLiteral("已创建空白模板预设"), 4000);
        });
    });
    connect(duplicatePreset, &QPushButton::clicked, this, [this] {
        if (m_templates.size() >= 32) { statusBar()->showMessage(QStringLiteral("最多支持 32 个模板预设"), 5000); return; }
        QJsonObject item = selectedTemplate();
        if (item.isEmpty()) return;
        const QString id = QUuid::createUuid().toString(QUuid::WithoutBraces);
        item.insert(QStringLiteral("id"), id);
        item.insert(QStringLiteral("name"), item.value(QStringLiteral("name")).toString().left(44) + QStringLiteral(" 副本"));
        item.insert(QStringLiteral("updatedAt"), QDateTime::currentDateTimeUtc().toString(Qt::ISODate));
        QJsonArray updated = m_templates;
        updated.append(item);
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this, id](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            const int index = m_template->findData(id);
            if (index >= 0) m_template->setCurrentIndex(index);
            m_templateName->setText(m_template->currentText());
            statusBar()->showMessage(QStringLiteral("当前模板已复制"), 4000);
        });
    });
    connect(deletePreset, &QPushButton::clicked, this, [this] {
        const QString id = m_template->currentData().toString();
        if (id.isEmpty()) return;
        if (m_templates.size() <= 1) { statusBar()->showMessage(QStringLiteral("至少需要保留一个模板预设"), 5000); return; }
        if (QMessageBox::question(this, QStringLiteral("删除模板预设"), QStringLiteral("确定删除当前模板预设吗？")) != QMessageBox::Yes) return;
        QJsonArray updated;
        for (const QJsonValue &value : m_templates) {
            if (value.toObject().value(QStringLiteral("id")).toString() != id) updated.append(value);
        }
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            statusBar()->showMessage(QStringLiteral("模板预设已删除"), 4000);
        });
    });
    txLayout->addWidget(templateStrip);

    auto *txMain = new QSplitter(Qt::Horizontal, txPane);
    txMain->setChildrenCollapsible(false);
    auto *txPreviewColumn = new QWidget(txMain);
    auto *txPreviewLayout = new QVBoxLayout(txPreviewColumn);
    txPreviewLayout->setContentsMargins(0, 0, 0, 0);
    txPreviewLayout->setSpacing(5);
    m_txPreview = makePreview(txPreviewColumn);
    m_txPreview->setMinimumSize(330, 230);
    m_txPreview->setStyleSheet(QStringLiteral("QLabel { background:#ffffff; color:#87939b; border:1px solid #9da6ad; border-radius:0; }"));
    txPreviewLayout->addWidget(m_txPreview, 4);
    auto *qsoBox = new QGroupBox(QStringLiteral("本次 QSO 信息"), txPreviewColumn);
    auto *qsoGrid = new QGridLayout(qsoBox);
    qsoGrid->setContentsMargins(7, 7, 7, 7);
    qsoGrid->setHorizontalSpacing(6);
    qsoGrid->setVerticalSpacing(4);
    auto addQsoField = [&](int row, int column, const QString &label, QLineEdit **target, const QString &initial = QString()) {
        auto *cell = new QWidget(qsoBox);
        auto *cellLayout = new QVBoxLayout(cell);
        cellLayout->setContentsMargins(0, 0, 0, 0);
        cellLayout->setSpacing(2);
        auto *caption = new QLabel(label, cell);
        caption->setStyleSheet(QStringLiteral("font-size:8pt; color:#68747d;"));
        auto *input = new QLineEdit(initial, cell);
        input->setMinimumWidth(70);
        cellLayout->addWidget(caption);
        cellLayout->addWidget(input);
        qsoGrid->addWidget(cell, row, column);
        *target = input;
    };
    addQsoField(0, 0, QStringLiteral("本台呼号"), &m_callsign);
    addQsoField(0, 1, QStringLiteral("对方呼号"), &m_hisCall, QStringLiteral("CQ"));
    addQsoField(0, 2, QStringLiteral("QTH"), &m_qth);
    addQsoField(1, 0, QStringLiteral("网格定位"), &m_locator);
    addQsoField(1, 1, QStringLiteral("RST / 报告"), &m_report, QStringLiteral("595"));
    addQsoField(1, 2, QStringLiteral("消息"), &m_message);
    auto *ax25Cell = new QWidget(qsoBox);
    auto *ax25Layout = new QVBoxLayout(ax25Cell);
    ax25Layout->setContentsMargins(0, 0, 0, 0);
    ax25Layout->setSpacing(2);
    ax25Layout->addWidget(new QLabel(QStringLiteral("AX.25 To"), ax25Cell));
    m_ax25To = new QLineEdit(QStringLiteral("CQ"), ax25Cell);
    ax25Layout->addWidget(m_ax25To);
    qsoGrid->addWidget(ax25Cell, 2, 0);
    auto *ssidCell = new QWidget(qsoBox);
    auto *ssidLayout = new QVBoxLayout(ssidCell);
    ssidLayout->setContentsMargins(0, 0, 0, 0);
    ssidLayout->setSpacing(2);
    ssidLayout->addWidget(new QLabel(QStringLiteral("Source SSID"), ssidCell));
    m_ax25SSID = new QSpinBox(ssidCell);
    m_ax25SSID->setRange(0, 15);
    m_ax25SSID->setValue(0);
    ssidLayout->addWidget(m_ax25SSID);
    qsoGrid->addWidget(ssidCell, 2, 1);
    qsoGrid->setColumnStretch(0, 1);
    qsoGrid->setColumnStretch(1, 1);
    qsoGrid->setColumnStretch(2, 1);
    txPreviewLayout->addWidget(qsoBox, 1);

    auto *templateEditor = new QGroupBox(QStringLiteral("模板组件 / 属性"), txMain);
    auto *editorLayout = new QVBoxLayout(templateEditor);
    m_templateComponentsSummary = new QLabel(QStringLiteral("选择模板后显示组件概要。"), templateEditor);
    m_templateComponentsSummary->setWordWrap(true);
    m_templateComponentsSummary->setStyleSheet(QStringLiteral("color:#6d7982; font-size:8pt;"));
    editorLayout->addWidget(m_templateComponentsSummary);
    m_templateComponentList = new QListWidget(templateEditor);
    m_templateComponentList->setViewMode(QListView::IconMode);
    m_templateComponentList->setFlow(QListView::LeftToRight);
    m_templateComponentList->setWrapping(true);
    m_templateComponentList->setResizeMode(QListView::Adjust);
    m_templateComponentList->setMovement(QListView::Static);
    m_templateComponentList->setGridSize(QSize(150, 42));
    m_templateComponentList->setMinimumHeight(86);
    m_templateComponentList->setMaximumHeight(122);
    m_templateComponentList->setStyleSheet(QStringLiteral("QListWidget { background:#f8f9fa; border:1px solid #c4cbd0; } QListWidget::item { padding:5px; border-bottom:1px solid #e1e5e8; } QListWidget::item:selected { background:#dceef5; color:#315c70; }"));
    editorLayout->addWidget(m_templateComponentList, 2);
    m_templatePropertiesTable = new QTableWidget(templateEditor);
    m_templatePropertiesTable->setColumnCount(2);
    m_templatePropertiesTable->setRowCount(0);
    m_templatePropertiesTable->setHorizontalHeaderLabels({QStringLiteral("属性"), QStringLiteral("值")});
    m_templatePropertiesTable->horizontalHeader()->setSectionResizeMode(0, QHeaderView::ResizeToContents);
    m_templatePropertiesTable->horizontalHeader()->setSectionResizeMode(1, QHeaderView::Stretch);
    m_templatePropertiesTable->verticalHeader()->hide();
    m_templatePropertiesTable->setEditTriggers(QAbstractItemView::NoEditTriggers);
    m_templatePropertiesTable->setSelectionMode(QAbstractItemView::NoSelection);
    m_templatePropertiesTable->setMaximumHeight(190);
    editorLayout->addWidget(m_templatePropertiesTable, 2);
    auto *editHint = new QLabel(QStringLiteral("模板支持文字、图库图像和上次 RX 图层。可打开组件编辑器调整位置、字号、颜色及图层顺序。"), templateEditor);
    editHint->setWordWrap(true);
    editHint->setStyleSheet(QStringLiteral("color:#687780; font-size:8pt; padding:5px; background:#e9eef1; border:1px solid #cbd3d8;"));
    editorLayout->addWidget(editHint);
    auto *editComponents = new QPushButton(QStringLiteral("打开组件编辑器…"), templateEditor);
    editorLayout->addWidget(editComponents);
    connect(m_templateComponentList, &QListWidget::currentRowChanged, this, [this](int row) {
        if (!m_templatePropertiesTable) return;
        m_templatePropertiesTable->setRowCount(0);
        const QJsonArray components = selectedTemplate().value(QStringLiteral("components")).toArray();
        if (row < 0 || row >= components.size()) return;
        const QJsonObject component = components.at(row).toObject();
        const QString type = component.value(QStringLiteral("type")).toString();
        const QList<QPair<QString, QString>> values = {
            {QStringLiteral("类型"), type == QStringLiteral("text") ? QStringLiteral("文字") : type == QStringLiteral("image") ? QStringLiteral("图库图像") : QStringLiteral("上次 RX 图像")},
            {QStringLiteral("内容 / 来源"), valueOr(component, QStringLiteral("text"), valueOr(component, QStringLiteral("assetId"), QStringLiteral("--")))},
            {QStringLiteral("X / Y"), QStringLiteral("%1% / %2%格").arg(component.value(QStringLiteral("x")).toDouble(), 0, 'f', 1).arg(component.value(QStringLiteral("y")).toDouble(), 0, 'f', 1)},
            {QStringLiteral("字号"), QString::number(component.value(QStringLiteral("fontSize")).toDouble(0), 'f', 0)},
            {QStringLiteral("缩放"), QStringLiteral("%1%").arg(component.value(QStringLiteral("scale")).toDouble(0), 0, 'f', 1)},
            {QStringLiteral("显示 / 描边"), QStringLiteral("%1 / %2").arg(component.value(QStringLiteral("visible")).toBool(true) ? QStringLiteral("显示") : QStringLiteral("隐藏"), component.value(QStringLiteral("outline")).toBool() ? QStringLiteral("开启") : QStringLiteral("关闭"))}
        };
        for (const auto &entry : values) {
            const int newRow = m_templatePropertiesTable->rowCount();
            m_templatePropertiesTable->insertRow(newRow);
            m_templatePropertiesTable->setItem(newRow, 0, new QTableWidgetItem(entry.first));
            m_templatePropertiesTable->setItem(newRow, 1, new QTableWidgetItem(entry.second));
        }
    });
    connect(editComponents, &QPushButton::clicked, this, [this] { editSelectedTemplate(); });
    editorLayout->addStretch();
    txMain->addWidget(txPreviewColumn);
    txMain->addWidget(templateEditor);
    txMain->setStretchFactor(0, 3);
    txMain->setStretchFactor(1, 2);
    txLayout->addWidget(txMain, 5);
    m_txProgress = new QProgressBar(txPane);
    m_txProgress->setRange(0, 1000);
    m_txProgress->setValue(0);
    m_txProgress->setTextVisible(false);
    txLayout->addWidget(m_txProgress);
    auto *txSpectrumTitle = new QHBoxLayout();
    txSpectrumTitle->addWidget(new QLabel(QStringLiteral("TX 频谱 / 瀑布图"), txPane));
    txSpectrumTitle->addStretch();
    auto *txSpectrumHint = new QLabel(QStringLiteral("输出 · 音频瀑布图"), txPane);
    txSpectrumHint->setStyleSheet(QStringLiteral("font-size:8pt; color:#919aa1;"));
    txSpectrumTitle->addWidget(txSpectrumHint);
    txLayout->addLayout(txSpectrumTitle);
    m_txSpectrumStatus = new QLabel(QStringLiteral("等待 TX 音频"), txPane);
    m_txSpectrumStatus->setStyleSheet(QStringLiteral("font-size:8pt; color:#77858f;"));
    txLayout->addWidget(m_txSpectrumStatus);
    m_txSpectrum = new SpectrumWaterfallWidget(txPane);
    m_txSpectrum->setMinimumHeight(95);
    m_txSpectrum->setMaximumHeight(155);
    m_txSpectrum->setStyleSheet(QStringLiteral("border:1px solid #7e8c96;"));
    txLayout->addWidget(m_txSpectrum, 1);
    auto *txFooter = new QLabel(QStringLiteral("OUTPUT  -∞ dBFS       PEAK  ---       MODE  SSDV"), txPane);
    txFooter->setStyleSheet(QStringLiteral("font-size:8pt; color:#859099; padding:2px;"));
    txLayout->addWidget(txFooter);

    split->addWidget(rxPane);
    split->addWidget(txPane);
    split->setStretchFactor(0, 1);
    split->setStretchFactor(1, 1);
    split->setSizes({760, 790});
    workLayout->addWidget(split, 1);
    auto *warning = new QLabel(QStringLiteral("协议编解码仍在开发验证中，当前版本不应直接用于空中发射。"), workbench);
    warning->setStyleSheet(QStringLiteral("background:#f7f0df; border-top:1px solid #e0d2ac; color:#8b7441; padding:5px 8px; font-size:8pt;"));
    workLayout->addWidget(warning);
    tabs->addTab(workbench, QStringLiteral("工作台"));

    // Album cards replace the data table. Each card keeps the image itself as
    // the primary navigation target, just like the original grid view.
    auto *albumPage = new QWidget(tabs);
    auto *albumLayout = new QVBoxLayout(albumPage);
    albumLayout->setContentsMargins(10, 9, 10, 8);
    albumLayout->setSpacing(8);
    auto *albumToolbar = new QHBoxLayout();
    auto *albumTitle = new QLabel(QStringLiteral("RX / TX HISTORY   历史相册"), albumPage);
    albumTitle->setStyleSheet(QStringLiteral("font-size:11pt; font-weight:700; color:#344b58;"));
    albumToolbar->addWidget(albumTitle);
    albumToolbar->addStretch();
    m_albumFilter = makeCombo(albumPage, {QStringLiteral("全部图像"), QStringLiteral("仅接收 RX"), QStringLiteral("仅发送 TX")});
    m_albumFilter->setMinimumWidth(125);
    auto *refresh = new QPushButton(QStringLiteral("刷新"), albumPage);
    auto *importImage = new QPushButton(QStringLiteral("导入图库图片"), albumPage);
    auto *exportAll = new QPushButton(QStringLiteral("导出相册到文件夹"), albumPage);
    albumToolbar->addWidget(m_albumFilter);
    albumToolbar->addWidget(refresh);
    albumToolbar->addWidget(importImage);
    albumToolbar->addWidget(exportAll);
    albumLayout->addLayout(albumToolbar);
    auto *albumScroll = new QScrollArea(albumPage);
    albumScroll->setWidgetResizable(true);
    albumScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    albumScroll->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    m_albumCardsHost = new QWidget(albumScroll);
    m_albumCardsLayout = new QGridLayout(m_albumCardsHost);
    m_albumCardsLayout->setContentsMargins(2, 2, 2, 2);
    m_albumCardsLayout->setSpacing(8);
    m_albumCardsLayout->setAlignment(Qt::AlignTop | Qt::AlignLeft);
    albumScroll->setWidget(m_albumCardsHost);
    albumLayout->addWidget(albumScroll, 1);
    auto *albumHint = new QLabel(QStringLiteral("点击缩略图预览；每张图像可单独导出或删除记录。"), albumPage);
    albumHint->setStyleSheet(QStringLiteral("font-size:8pt; color:#7d8991;"));
    albumLayout->addWidget(albumHint);
    connect(refresh, &QPushButton::clicked, this, [this] { refreshAlbum(); refreshGallery(); });
    connect(importImage, &QPushButton::clicked, this, [this] { importGalleryImage(); });
    connect(exportAll, &QPushButton::clicked, this, [this] { exportAlbum(); });
    connect(m_albumFilter, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) { refreshAlbum(); });
    tabs->addTab(albumPage, QStringLiteral("相册"));

    root->addWidget(tabs, 1);
    setCentralWidget(central);
    statusBar()->showMessage(QStringLiteral("Go 后端通过本机回环 RPC 连接；RX / TX 频谱分别显示。"));

    // Hidden log storage preserves diagnostic messages without adding a
    // permanent third pane to the operator's workbench.
    m_log = new QTextEdit(this);
    m_log->hide();
}

void MainWindow::loadInitialState() {
    QString error;
    const auto profiles = m_backend->callSync(QStringLiteral("GetProfiles"), {}, &error);
    if (!error.isEmpty()) {
        appendLog(QStringLiteral("读取模式列表失败：") + error);
        m_audioStatus->setText(QStringLiteral("模式列表不可用"));
        statusBar()->showMessage(QStringLiteral("初始化失败：%1").arg(error));
        return;
    }
    m_profiles = profiles.toArray();
    m_rxProfile->clear();
    m_rxProfile->addItem(QStringLiteral("自动识别"), QStringLiteral("auto"));
    m_txProfile->clear();
    for (const auto &value : m_profiles) {
        const auto profile = value.toObject();
        const QString label = valueOr(profile, QStringLiteral("name"), profile.value(QStringLiteral("id")).toString());
        const QString id = profile.value(QStringLiteral("id")).toString();
        m_rxProfile->addItem(label, id);
        m_txProfile->addItem(label, id);
    }
    const auto settings = m_backend->callSync(QStringLiteral("GetSettings"), {}, &error).toObject();
    if (!error.isEmpty()) {
        appendLog(QStringLiteral("读取设置失败：") + error);
        statusBar()->showMessage(QStringLiteral("设置读取失败，使用默认值：%1").arg(error));
    }
    m_settings = settings;
    m_callsign->setText(valueOr(settings, QStringLiteral("callsign"), QStringLiteral("N0CALL")));
    m_locator->setText(valueOr(settings, QStringLiteral("locator"), QStringLiteral("JJ00")));
    m_ax25SSID->setValue(settings.value(QStringLiteral("ax25Ssid")).toInt(0));
    const QString rx = valueOr(settings, QStringLiteral("rxProfile"), QStringLiteral("auto"));
    const QString tx = valueOr(settings, QStringLiteral("txProfile"), QStringLiteral("ssdv-ax25"));
    int idx = m_rxProfile->findData(rx); if (idx < 0) idx = 0; m_rxProfile->setCurrentIndex(idx);
    idx = m_txProfile->findData(tx); if (idx < 0 && m_txProfile->count() > 0) idx = 0; if (idx >= 0) m_txProfile->setCurrentIndex(idx);

    auto addProfileButton = [this](QHBoxLayout *layout, QComboBox *selector, const QString &settingKey,
                                   const QString &id, const QString &label, bool selected) {
        auto *button = new QPushButton(label, layout->parentWidget());
        button->setCheckable(true);
        button->setAutoExclusive(true);
        button->setMinimumHeight(29);
        button->setMinimumWidth(id == QStringLiteral("auto") ? 96 : 104);
        button->setChecked(selected);
        layout->addWidget(button);
        connect(button, &QPushButton::clicked, this, [this, selector, settingKey, id]() {
            const int index = selector->findData(id);
            if (index >= 0) selector->setCurrentIndex(index);
            m_settings.insert(settingKey, id);
            saveSettings(m_settings);
        });
    };
    addProfileButton(m_rxProfileButtons, m_rxProfile, QStringLiteral("rxProfile"), QStringLiteral("auto"),
                     QStringLiteral("自动识别"), m_rxProfile->currentData().toString() == QStringLiteral("auto"));
    for (const auto &value : m_profiles) {
        const auto profile = value.toObject();
        const QString id = profile.value(QStringLiteral("id")).toString();
        const QString label = valueOr(profile, QStringLiteral("name"), id);
        addProfileButton(m_rxProfileButtons, m_rxProfile, QStringLiteral("rxProfile"), id, label, m_rxProfile->currentData().toString() == id);
        addProfileButton(m_txProfileButtons, m_txProfile, QStringLiteral("txProfile"), id, label, m_txProfile->currentData().toString() == id);
    }
    const QString quality = valueOr(settings, QStringLiteral("quality"), QStringLiteral("medium"));
    m_quality->setCurrentIndex(quality == QStringLiteral("low") ? 0 : quality == QStringLiteral("high") ? 2 : 1);
    connect(m_quality, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int index) { m_settings.insert(QStringLiteral("quality"),index==0?QStringLiteral("low"):index==2?QStringLiteral("high"):QStringLiteral("medium")); saveSettings(m_settings); });
    refreshTemplates();
    refreshGallery();
    refreshAlbum();
    QString initialTemplateError;
    const QImage initialTemplate = renderTemplate(&initialTemplateError);
    if (!initialTemplate.isNull()) m_txPreview->setSourceImage(initialTemplate);
    else if (!initialTemplateError.isEmpty()) appendLog(initialTemplateError);
    refreshStatus();
}

void MainWindow::refreshLatestRXPreview() {
    if (m_rxPreviewRequestPending) {
        m_rxPreviewRefreshQueued = true;
        return;
    }
    m_rxPreviewRequestPending = true;
    m_rxPreviewRefreshQueued = false;
    m_backend->call(QStringLiteral("GetLatestRXImageData"), {}, [this](bool ok, const QJsonValue &value, const QString &error) {
        m_rxPreviewRequestPending = false;
        if (ok && !value.toString().isEmpty()) {
            showImage(value.toString(), m_rxPreview);
        } else if (!ok) {
            appendLog(QStringLiteral("刷新接收图像预览失败：") + error);
        }
        if (m_rxPreviewRefreshQueued) refreshLatestRXPreview();
    });
}

void MainWindow::refreshStatus() {
    if (m_statusRequestPending) return;
    m_statusRequestPending = true;
    m_backend->call(QStringLiteral("GetAudioState"), {}, [this](bool ok, const QJsonValue &value, const QString &error) {
        m_statusRequestPending = false;
        if (!ok) {
            m_audioStatus->setText(QStringLiteral("音频后端异常"));
            m_audioStatus->setToolTip(error);
            m_audioStatus->setStyleSheet(QStringLiteral("background:#4d2730; padding:8px; border-radius:6px;"));
            return;
        }
        const QJsonObject state = value.toObject();
        const bool connected = state.value(QStringLiteral("connected")).toBool();
        const bool rx = state.value(QStringLiteral("rxActive")).toBool();
        const bool tx = state.value(QStringLiteral("txActive")).toBool();
        const int rate = state.value(QStringLiteral("sampleRate")).toInt();
        m_audioStatus->setText(QStringLiteral("音频：%1 · %2 Hz · RX %3 / TX %4")
            .arg(connected ? QStringLiteral("已连接") : QStringLiteral("未初始化"))
            .arg(rate)
            .arg(rx ? QStringLiteral("运行") : QStringLiteral("待机"))
            .arg(tx ? QStringLiteral("运行") : QStringLiteral("待机")));
        m_audioStatus->setStyleSheet(connected
            ? QStringLiteral("background:#173b32; border:1px solid #2c8b6a; padding:8px 12px; border-radius:6px;")
            : QStringLiteral("background:#3d3030; border:1px solid #805050; padding:8px 12px; border-radius:6px;"));
        m_audioStatus->setToolTip(state.value(QStringLiteral("error")).toString());
    });
}

void MainWindow::refreshTemplates() {
    QString error; const auto result=m_backend->callSync(QStringLiteral("GetTemplates"),{},&error);
    if(!error.isEmpty()){appendLog(error);return;} m_templates=result.toArray();
    const QString current=m_template->currentData().toString(); m_template->clear();
    for(const auto &item:m_templates){const auto tpl=item.toObject();m_template->addItem(tpl.value(QStringLiteral("name")).toString(),tpl.value(QStringLiteral("id")).toString());}
    int index = m_template->findData(current);
    if (index < 0 && m_template->count() > 0) index = 0;
    if (index >= 0) m_template->setCurrentIndex(index);
    if (m_templateName) m_templateName->setText(m_template->currentText());
    refreshTemplateComponentList();
}

void MainWindow::refreshTemplateComponentList() {
    const QJsonObject selected = selectedTemplate();
    const QJsonArray components = selected.value(QStringLiteral("components")).toArray();
    if (m_templateComponentsSummary) {
        m_templateComponentsSummary->setText(QStringLiteral("%1 · %2 个组件 · 可打开组件编辑器调整图层、文本和图像属性。")
            .arg(selected.value(QStringLiteral("name")).toString(), QString::number(components.size())));
    }
    if (!m_templateComponentList) return;
    m_templateComponentList->clear();
    for (int i = 0; i < components.size(); ++i) {
        const QJsonObject component = components.at(i).toObject();
        const QString type = component.value(QStringLiteral("type")).toString();
        const QString label = type == QStringLiteral("text")
            ? QStringLiteral("T · %1").arg(valueOr(component, QStringLiteral("text"), QStringLiteral("文字组件")))
            : type == QStringLiteral("image") ? QStringLiteral("IMG · %1").arg(valueOr(component, QStringLiteral("assetId"), QStringLiteral("图库图像")))
            : QStringLiteral("RX · 上次接收图像");
        auto *item = new QListWidgetItem(label, m_templateComponentList);
        item->setData(Qt::UserRole, i);
    }
    if (!components.isEmpty()) m_templateComponentList->setCurrentRow(0);
    else if (m_templatePropertiesTable) m_templatePropertiesTable->setRowCount(0);
}

void MainWindow::refreshGallery() {
    QString error; const auto result=m_backend->callSync(QStringLiteral("GetGallery"),{},&error);
    if(error.isEmpty())m_gallery=result.toArray(); else appendLog(error);
}

void MainWindow::refreshAlbum() {
    if (!m_albumCardsLayout || !m_albumCardsHost || !m_backend || !m_backend->isRunning()) return;
    QString error;
    const auto result = m_backend->callSync(QStringLiteral("GetAlbum"), {}, &error, 10000);
    if (!error.isEmpty()) {
        appendLog(QStringLiteral("读取相册失败：") + error);
        statusBar()->showMessage(QStringLiteral("读取相册失败：%1").arg(error), 6000);
        return;
    }
    m_album = result.toArray();

    while (QLayoutItem *item = m_albumCardsLayout->takeAt(0)) {
        if (QWidget *widget = item->widget()) widget->deleteLater();
        delete item;
    }

    const int filter = m_albumFilter ? m_albumFilter->currentIndex() : 0;
    const int availableWidth = m_albumCardsHost->width() > 0 ? m_albumCardsHost->width() : 1480;
    const int columns = qBound(1, availableWidth / 210, 7);
    int visibleRow = 0;
    int visibleColumn = 0;

    for (const QJsonValue &entry : m_album) {
        const QJsonObject record = entry.toObject();
        const QString type = record.value(QStringLiteral("type")).toString().toUpper();
        if (filter == 1 && type != QStringLiteral("RX")) continue;
        if (filter == 2 && type != QStringLiteral("TX")) continue;
        const QString id = record.value(QStringLiteral("id")).toString();
        if (id.isEmpty()) continue;

        auto *card = new QFrame(m_albumCardsHost);
        card->setMinimumWidth(198);
        card->setMaximumWidth(260);
        card->setStyleSheet(QStringLiteral("QFrame { background:#f8f9fa; border:1px solid #c4cbd0; }"));
        auto *cardLayout = new QVBoxLayout(card);
        cardLayout->setContentsMargins(6, 6, 6, 6);
        cardLayout->setSpacing(4);

        auto *thumb = new QPushButton(card);
        thumb->setMinimumHeight(112);
        thumb->setMaximumHeight(135);
        thumb->setCursor(Qt::PointingHandCursor);
        thumb->setStyleSheet(QStringLiteral("QPushButton { background:#ffffff; border:1px solid #d0d6da; padding:1px; }"));
        thumb->setText(QStringLiteral("正在读取图像…"));
        cardLayout->addWidget(thumb);
        QPointer<QPushButton> safeThumb(thumb);
        m_backend->call(QStringLiteral("GetAlbumImageData"), QJsonArray{id}, [this, safeThumb](bool ok, const QJsonValue &value, const QString &) {
            if (!safeThumb || !ok) return;
            const QString dataUrl = value.toString();
            const QImage thumbnail = imageFromDataUrl(dataUrl);
            if (thumbnail.isNull()) {
                safeThumb->setText(QStringLiteral("图像预览不可用"));
                return;
            }
            safeThumb->setProperty("imageDataUrl", dataUrl);
            safeThumb->setText(QString());
            safeThumb->setIcon(QIcon(QPixmap::fromImage(thumbnail).scaled(220, 128, Qt::KeepAspectRatio, Qt::SmoothTransformation)));
            safeThumb->setIconSize(QSize(220, 128));
        });
        connect(thumb, &QPushButton::clicked, this, [this, id, thumb]() {
            auto showPreview = [this](const QString &dataUrl) {
                const QImage previewImage = imageFromDataUrl(dataUrl);
                if (previewImage.isNull()) { statusBar()->showMessage(QStringLiteral("无法解码相册图像"), 6000); return; }
                QDialog preview(this);
                preview.setWindowTitle(QStringLiteral("图像预览"));
                auto *layout = new QVBoxLayout(&preview);
                auto *label = new QLabel(&preview);
                label->setAlignment(Qt::AlignCenter);
                label->setPixmap(QPixmap::fromImage(previewImage).scaled(1200, 850, Qt::KeepAspectRatio, Qt::SmoothTransformation));
                layout->addWidget(label);
                preview.resize(qMin(1260, previewImage.width() + 40), qMin(920, previewImage.height() + 80));
                preview.exec();
            };
            const QString cached = thumb->property("imageDataUrl").toString();
            if (!cached.isEmpty()) { showPreview(cached); return; }
            m_backend->call(QStringLiteral("GetAlbumImageData"), QJsonArray{id}, [this, showPreview](bool ok, const QJsonValue &value, const QString &error) {
                if (!ok) { statusBar()->showMessage(error, 6000); return; }
                showPreview(value.toString());
            });
        });

        auto *badge = new QLabel(type == QStringLiteral("RX") ? QStringLiteral("RX · RECEIVED") : QStringLiteral("TX · SENT"), card);
        badge->setStyleSheet(type == QStringLiteral("RX")
            ? QStringLiteral("background:#dcecf3; border:1px solid #a7c6d5; color:#315c70; padding:2px 5px; font-size:8pt; font-weight:700;")
            : QStringLiteral("background:#f7eddb; border:1px solid #ddc99f; color:#8a6732; padding:2px 5px; font-size:8pt; font-weight:700;"));
        badge->setMaximumWidth(110);
        cardLayout->addWidget(badge, 0, Qt::AlignLeft);

        const QString profile = record.value(QStringLiteral("profile")).toString();
        auto *name = new QLabel(profile, card);
        name->setStyleSheet(QStringLiteral("font-size:9pt; font-weight:700; color:#344b58;"));
        name->setWordWrap(true);
        cardLayout->addWidget(name);

        QDateTime created = QDateTime::fromString(record.value(QStringLiteral("createdAt")).toString(), Qt::ISODateWithMs);
        if (!created.isValid()) created = QDateTime::fromString(record.value(QStringLiteral("createdAt")).toString(), Qt::ISODate);
        const QString timeText = created.isValid() ? created.toLocalTime().toString(QStringLiteral("yyyy/MM/dd HH:mm:ss")) : record.value(QStringLiteral("createdAt")).toString();
        const QString callsign = valueOr(record, QStringLiteral("callsign"), QStringLiteral("---"));
        const QString packets = QStringLiteral("%1/%2").arg(record.value(QStringLiteral("packetsReceived")).toInt()).arg(record.value(QStringLiteral("packetsExpected")).toInt());
        const QString quality = valueOr(record, QStringLiteral("quality"), QStringLiteral("-"));
        auto *metadata = new QLabel(QStringLiteral("%1\n%2 · ID %3 · %4 · %5")
            .arg(timeText, callsign)
            .arg(record.value(QStringLiteral("imageId")).toInt())
            .arg(packets, record.value(QStringLiteral("status")).toString()), card);
        metadata->setWordWrap(true);
        metadata->setStyleSheet(QStringLiteral("font-size:8pt; color:#74808a;"));
        cardLayout->addWidget(metadata);
        if (quality != QStringLiteral("-")) {
            auto *qualityLabel = new QLabel(QStringLiteral("质量：%1").arg(quality), card);
            qualityLabel->setStyleSheet(QStringLiteral("font-size:8pt; color:#89939b;"));
            cardLayout->addWidget(qualityLabel);
        }

        auto *actions = new QHBoxLayout();
        auto *exportButton = new QPushButton(QStringLiteral("导出文件"), card);
        auto *deleteButton = new QPushButton(QStringLiteral("删除记录"), card);
        deleteButton->setProperty("role", "danger");
        actions->addWidget(exportButton);
        actions->addStretch();
        actions->addWidget(deleteButton);
        cardLayout->addLayout(actions);
        connect(exportButton, &QPushButton::clicked, this, [this, id]() {
            const QString dir = QFileDialog::getExistingDirectory(this, QStringLiteral("导出图像"), QStandardPaths::writableLocation(QStandardPaths::PicturesLocation));
            if (dir.isEmpty()) return;
            m_backend->call(QStringLiteral("ExportAlbumRecordToDirectory"), QJsonArray{id, dir}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (ok) statusBar()->showMessage(QStringLiteral("相册图像已导出"), 4000);
                else showError(error);
            });
        });
        connect(deleteButton, &QPushButton::clicked, this, [this, id]() {
            if (QMessageBox::question(this, QStringLiteral("删除图像记录"), QStringLiteral("确定删除所选相册记录吗？")) != QMessageBox::Yes) return;
            m_backend->call(QStringLiteral("DeleteAlbumRecord"), QJsonArray{id}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (ok) refreshAlbum(); else showError(error);
            });
        });

        m_albumCardsLayout->addWidget(card, visibleRow, visibleColumn);
        ++visibleColumn;
        if (visibleColumn >= columns) { visibleColumn = 0; ++visibleRow; }
    }

    if (visibleRow == 0 && visibleColumn == 0) {
        auto *empty = new QLabel(QStringLiteral("相册暂无图像。"), m_albumCardsHost);
        empty->setAlignment(Qt::AlignCenter);
        empty->setMinimumHeight(180);
        empty->setStyleSheet(QStringLiteral("color:#87939b; border:1px dashed #c4cbd0; background:#f4f6f7;"));
        m_albumCardsLayout->addWidget(empty, 0, 0, 1, columns);
    }
    m_albumCardsLayout->setColumnStretch(columns, 1);
}

void MainWindow::pollEvents() {
    if(!m_backend->isRunning())return;
    m_backend->pollEvents(m_eventSequence,[this](const QJsonArray &events,quint64 next,bool reset,const QString &error){
        if(!error.isEmpty()){return;} if(reset){m_eventSequence=0;}
        for(const auto &item:events)handleEvent(item.toObject()); m_eventSequence=next;
    });
}

void MainWindow::handleEvent(const QJsonObject &event) {
    const QString name=event.value(QStringLiteral("name")).toString(); const QJsonValue payloadValue=event.value(QStringLiteral("payload"));
    if(name==QStringLiteral("rx:image")){
        const auto payload=payloadValue.toObject();
        if(payload.value(QStringLiteral("hasPreview")).toBool()) refreshLatestRXPreview();
        const QString profile = payload.value(QStringLiteral("profile")).toString();
        const QString callsign = payload.value(QStringLiteral("callsign")).toString();
        const int received = payload.value(QStringLiteral("packetsReceived")).toInt();
        const int expected = payload.value(QStringLiteral("packetsExpected")).toInt();
        const int imageId = payload.value(QStringLiteral("imageId")).toInt();
        const double coverage = payload.value(QStringLiteral("coverage")).toDouble() * 100.0;
        const bool complete = payload.value(QStringLiteral("complete")).toBool();
        m_rxStatus->setText(QStringLiteral("%1 · %2 · %3% · %4")
            .arg(profile, callsign).arg(coverage, 0, 'f', 1).arg(complete ? QStringLiteral("完成") : QStringLiteral("接收中")));
        m_rxMeta->setText(QStringLiteral("CALL  %1      IMAGE ID  %2      PACKETS  %3 / %4      MODE  %5")
            .arg(callsign).arg(imageId).arg(received).arg(expected).arg(profile));
        if(payload.value(QStringLiteral("complete")).toBool())refreshAlbum();
    } else if(name==QStringLiteral("rx:level")){
        const auto p=payloadValue.toObject();m_rxStatus->setToolTip(QStringLiteral("RMS %1 dBFS · Peak %2").arg(p.value(QStringLiteral("rmsDbfs")).toDouble(),0,'f',1).arg(p.value(QStringLiteral("peak")).toDouble(),0,'f',3));
    } else if(name==QStringLiteral("rx:spectrum")||name==QStringLiteral("tx:spectrum")){
        const auto bins=payloadValue.toArray(); if(!bins.isEmpty()){
            SpectrumWaterfallWidget *target = name.startsWith(QStringLiteral("rx")) ? m_rxSpectrum : m_txSpectrum;
            QLabel *status = name.startsWith(QStringLiteral("rx")) ? m_rxSpectrumStatus : m_txSpectrumStatus;
            target->pushBins(bins);
            double peak=-120.0;int peakHz=0;for(const auto &b:bins){const auto o=b.toObject();const double db=o.value(QStringLiteral("magnitudeDbfs")).toDouble(-120);if(db>peak){peak=db;peakHz=o.value(QStringLiteral("frequencyHz")).toInt();}}
            status->setText(QStringLiteral("%1 bins · 峰值 %2 dBFS @ %3 Hz").arg(bins.size()).arg(peak,0,'f',1).arg(peakHz));
        }
    } else if(name==QStringLiteral("tx:progress")){
        m_txProgress->setValue(static_cast<int>(payloadValue.toDouble()*1000.0));
    } else if(name==QStringLiteral("tx:state")){
        if(!payloadValue.toObject().value(QStringLiteral("active")).toBool()){m_txProgress->setValue(0);refreshAlbum();}
    } else if(name==QStringLiteral("tx:status")){
        const auto p=payloadValue.toObject();appendLog(QStringLiteral("TX: ")+QJsonDocument(p).toJson(QJsonDocument::Compact));
    } else if(name.endsWith(QStringLiteral(":error"))||name==QStringLiteral("ptt:error")||name==QStringLiteral("audio:error")){
        showError(payloadValue.isString()?payloadValue.toString():QJsonDocument(payloadValue.toObject()).toJson(QJsonDocument::Compact));
    }
}

void MainWindow::startRX() {
    m_settings.insert(QStringLiteral("rxProfile"), m_rxProfile->currentData().toString());
    QString saveError;
    m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &saveError, 15000);
    if (!saveError.isEmpty()) { showError(saveError); return; }
    m_backend->call(QStringLiteral("StartRX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
        if (ok) { m_rxStatus->setText(QStringLiteral("RX 已启动")); appendLog(QStringLiteral("接收已启动")); }
        else showError(error);
    });
}
void MainWindow::stopRX() {
    m_backend->call(QStringLiteral("StopRX"),{},[this](bool ok,const QJsonValue&,const QString &error){if(ok){m_rxStatus->setText(QStringLiteral("RX 已停止"));appendLog(QStringLiteral("接收已停止"));}else showError(error);});
}

QJsonObject MainWindow::selectedTemplate() const {
    const QString id=m_template->currentData().toString();for(const auto &item:m_templates){const auto tpl=item.toObject();if(tpl.value(QStringLiteral("id")).toString()==id)return tpl;}return {};
}
QString MainWindow::selectedQuality() const {return m_quality->currentIndex()==0?QStringLiteral("low"):m_quality->currentIndex()==2?QStringLiteral("high"):QStringLiteral("medium");}

QString MainWindow::resolveTemplateText(QString text) const {
    const QString now=QDateTime::currentDateTime().toString(QStringLiteral("yyyy-MM-dd HH:mm"));
    const QList<QPair<QString,QString>> replacements={{QStringLiteral("{MYCALL}"),m_callsign->text()},{QStringLiteral("{CALLSIGN}"),m_callsign->text()},{QStringLiteral("{HISCALL}"),m_hisCall->text()},{QStringLiteral("{LOCATOR}"),m_locator->text()},{QStringLiteral("{REPORT}"),m_report->text()},{QStringLiteral("{QTH}"),m_qth->text()},{QStringLiteral("{MESSAGE}"),m_message->text()},{QStringLiteral("{DATE}"),QDate::currentDate().toString(Qt::ISODate)},{QStringLiteral("{TIME}"),QTime::currentTime().toString(QStringLiteral("HH:mm"))},{QStringLiteral("{DATETIME}"),now}};
    for(const auto &replacement:replacements)text.replace(replacement.first,replacement.second,Qt::CaseInsensitive);return text;
}

QByteArray MainWindow::imageDataUrlBytes(const QString &dataUrl) const {
    const int comma=dataUrl.indexOf(','); if(comma<0)return {};const QByteArray header=dataUrl.left(comma).toLatin1();const QByteArray body=dataUrl.mid(comma+1).toLatin1();return header.contains(";base64")?QByteArray::fromBase64(body):QByteArray::fromPercentEncoding(body);
}
QImage MainWindow::imageFromDataUrl(const QString &dataUrl) const {QImage image;image.loadFromData(imageDataUrlBytes(dataUrl));return image;}

QImage MainWindow::renderTemplate(QString *errorMessage) {
    const QSize size=m_quality->currentIndex()==0?QSize(320,240):m_quality->currentIndex()==2?QSize(640,480):QSize(448,336);
    QImage image(size,QImage::Format_ARGB32_Premultiplied);image.fill(QColor(QStringLiteral("#17232d")));
    QPainter painter(&image);painter.setRenderHint(QPainter::Antialiasing);painter.setRenderHint(QPainter::TextAntialiasing);painter.setRenderHint(QPainter::SmoothPixmapTransform);
    QJsonObject tpl=selectedTemplate();
    const QString backgroundId=valueOr(m_settings,QStringLiteral("templateBackgroundAssetId"),tpl.value(QStringLiteral("backgroundAssetId")).toString());
    if(!backgroundId.isEmpty()){
        QString error;const auto data=m_backend->callSync(QStringLiteral("GetGalleryImageData"),QJsonArray{backgroundId},&error,10000).toString();
        if(error.isEmpty()){QImage bg=imageFromDataUrl(data);if(!bg.isNull()){const QSizeF target=size;const qreal factor=qMax(target.width()/bg.width(),target.height()/bg.height());QSizeF scaled(bg.width()*factor,bg.height()*factor);painter.drawImage(QRectF((target.width()-scaled.width())/2,(target.height()-scaled.height())/2,scaled.width(),scaled.height()),bg);}}
    }
    const QJsonArray components=tpl.value(QStringLiteral("components")).toArray();
    for(const auto &value:components){const auto c=value.toObject();if(c.contains(QStringLiteral("visible"))&&!c.value(QStringLiteral("visible")).toBool())continue;
        const QString type=c.value(QStringLiteral("type")).toString();const qreal x=size.width()*c.value(QStringLiteral("x")).toDouble()/100.0;const qreal y=size.height()*c.value(QStringLiteral("y")).toDouble()/100.0;
        if(type==QStringLiteral("text")){
            const qreal scale=size.height()/480.0;const qreal fs=qMax(6.0,c.value(QStringLiteral("fontSize")).toDouble(24)*scale);QFont font(QStringLiteral("Segoe UI"));font.setPixelSize(qRound(fs));font.setBold(c.value(QStringLiteral("bold")).toBool());painter.setFont(font);
            const QString text=resolveTemplateText(c.value(QStringLiteral("text")).toString());const QColor fill(valueOr(c,QStringLiteral("color"),QStringLiteral("#ffffff")));
            const QColor outline(valueOr(c,QStringLiteral("outlineColor"),QStringLiteral("#000000")));const qreal lineWidth=c.value(QStringLiteral("outlineWidth")).toDouble(2)*scale;
            QStringList lines=text.split('\n');qreal yy=y;for(const QString &line:lines){if(c.value(QStringLiteral("outline")).toBool(true)&&lineWidth>0){QPainterPath path;path.addText(QPointF(x,yy+fs),font,line);painter.strokePath(path,QPen(outline,lineWidth*2,Qt::SolidLine,Qt::RoundCap,Qt::RoundJoin));painter.fillPath(path,fill);}else painter.setPen(fill),painter.drawText(QPointF(x,yy+fs),line);yy+=fs*1.22;}
        }else if(type==QStringLiteral("image")||type==QStringLiteral("last-rx-image")){
            QString data;QString error;if(type==QStringLiteral("image")){data=m_backend->callSync(QStringLiteral("GetGalleryImageData"),QJsonArray{c.value(QStringLiteral("assetId")).toString()},&error,10000).toString();}else{data=m_backend->callSync(QStringLiteral("GetLatestRXImageData"),{},&error,10000).toString();}
            QImage component=imageFromDataUrl(data);if(component.isNull()){if(type==QStringLiteral("last-rx-image")){QRectF frame(x,y,size.width()*0.35,size.height()*0.35*0.75);painter.fillRect(frame,QColor(18,30,40,220));painter.setPen(QColor(QStringLiteral("#d8e0e5")));painter.drawText(frame,Qt::AlignCenter,QStringLiteral("LAST RX"));}continue;}
            const qreal fit=qMin(size.width()/qreal(component.width()),size.height()/qreal(component.height()));const qreal factor=fit*qBound(0.01,c.value(QStringLiteral("scale")).toDouble(35)/100.0,1.0);const QSizeF target(component.width()*factor,component.height()*factor);QRectF rect(x,y,target.width(),target.height());
            if(c.value(QStringLiteral("outline")).toBool()&&c.value(QStringLiteral("outlineWidth")).toDouble()>0){painter.setPen(QPen(QColor(valueOr(c,QStringLiteral("outlineColor"),QStringLiteral("#000000"))),c.value(QStringLiteral("outlineWidth")).toDouble()*size.height()/480.0));painter.drawRect(rect.adjusted(-1,-1,1,1));}
            painter.drawImage(rect,component);
        }
    }
    painter.end();if(errorMessage)errorMessage->clear();return image;
}

void MainWindow::startTX() {
    m_settings.insert(QStringLiteral("callsign"), m_callsign->text().trimmed());
    m_settings.insert(QStringLiteral("locator"), m_locator->text().trimmed());
    m_settings.insert(QStringLiteral("txProfile"), m_txProfile->currentData().toString());
    m_settings.insert(QStringLiteral("quality"), selectedQuality());
    m_settings.insert(QStringLiteral("ax25Ssid"), m_ax25SSID->value());

    QString saveError;
    m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &saveError, 15000);
    if (!saveError.isEmpty()) { showError(saveError); return; }

    QString renderError;
    QImage image = renderTemplate(&renderError);
    if (image.isNull()) {
        showError(renderError.isEmpty() ? QStringLiteral("无法生成发送图像") : renderError);
        return;
    }
    QByteArray png;
    QBuffer buffer(&png);
    if (!buffer.open(QIODevice::WriteOnly) || !image.save(&buffer, "PNG")) {
        showError(QStringLiteral("无法将发送模板编码为 PNG"));
        return;
    }
    m_txPreview->setSourceImage(image);

    QJsonObject rendered;
    rendered.insert(QStringLiteral("id"), QStringLiteral("qt-%1").arg(QDateTime::currentMSecsSinceEpoch()));
    rendered.insert(QStringLiteral("name"), QStringLiteral("Qt Native Template"));
    rendered.insert(QStringLiteral("pngBase64"), QString::fromLatin1(png.toBase64()));
    const QJsonArray args{rendered, m_repeat->isChecked(), m_repeatInterval->value(), m_ax25To->text().trimmed()};
    m_backend->call(QStringLiteral("StartTXRendered"), args, [this](bool ok, const QJsonValue &, const QString &error) {
        if (ok) { m_txProgress->setValue(0); appendLog(QStringLiteral("已提交图像发射任务")); }
        else showError(error);
    });
}

void MainWindow::saveSettings(const QJsonObject &settings) {
    m_backend->call(QStringLiteral("SaveSettings"),QJsonArray{settings},[this](bool ok,const QJsonValue&,const QString &error){if(!ok)appendLog(QStringLiteral("保存设置失败：")+error);});
}

void MainWindow::openSettings() {
    QString error;
    QJsonObject settings = m_backend->callSync(QStringLiteral("GetSettings"), {}, &error).toObject();
    if (!error.isEmpty()) { showError(error); return; }

    QDialog dialog(this);
    dialog.setWindowTitle(QStringLiteral("设备与电台设置"));
    dialog.resize(620, 760);
    auto *layout = new QVBoxLayout(&dialog);
    auto *form = new QFormLayout();

    auto *callsign = new QLineEdit(settings.value(QStringLiteral("callsign")).toString(), &dialog);
    auto *locator = new QLineEdit(settings.value(QStringLiteral("locator")).toString(), &dialog);
    auto *language = makeCombo(&dialog, {QStringLiteral("简体中文"), QStringLiteral("English"), QStringLiteral("日本語")});
    const QString locale = settings.value(QStringLiteral("language")).toString();
    language->setCurrentIndex(locale == QStringLiteral("en") ? 1 : locale == QStringLiteral("ja") ? 2 : 0);

    auto *sample = makeCombo(&dialog, {QStringLiteral("19200"), QStringLiteral("44100"), QStringLiteral("48000"), QStringLiteral("96000")});
    const int rateIndex = sample->findText(QString::number(settings.value(QStringLiteral("audioSampleRate")).toInt(48000)));
    if (rateIndex >= 0) sample->setCurrentIndex(rateIndex);

    auto *rxDevice = new QComboBox(&dialog);
    auto *txDevice = new QComboBox(&dialog);
    rxDevice->addItem(QStringLiteral("系统默认"), QString());
    txDevice->addItem(QStringLiteral("系统默认"), QString());
    const auto devices = m_backend->callSync(QStringLiteral("GetAudioDevices"), {}, &error, 15000).toObject();
    if (error.isEmpty()) {
        for (const auto &v : devices.value(QStringLiteral("inputs")).toArray()) {
            const auto item = v.toObject();
            rxDevice->addItem(item.value(QStringLiteral("name")).toString(), item.value(QStringLiteral("id")).toString());
        }
        for (const auto &v : devices.value(QStringLiteral("outputs")).toArray()) {
            const auto item = v.toObject();
            txDevice->addItem(item.value(QStringLiteral("name")).toString(), item.value(QStringLiteral("id")).toString());
        }
    } else {
        appendLog(QStringLiteral("音频设备列表不可用：") + error);
    }
    int index = rxDevice->findData(settings.value(QStringLiteral("rxInputDevice")).toString());
    if (index >= 0) rxDevice->setCurrentIndex(index);
    index = txDevice->findData(settings.value(QStringLiteral("txOutputDevice")).toString());
    if (index >= 0) txDevice->setCurrentIndex(index);

    const QJsonObject ptt = settings.value(QStringLiteral("ptt")).toObject();
    auto *pttMode = makeCombo(&dialog, {QStringLiteral("VOX"), QStringLiteral("Hamlib Net Rigctl"), QStringLiteral("串口 RTS"), QStringLiteral("串口 DTR")});
    const QString mode = ptt.value(QStringLiteral("mode")).toString();
    pttMode->setCurrentIndex(mode == QStringLiteral("hamlib") ? 1 : mode == QStringLiteral("rts") ? 2 : mode == QStringLiteral("dtr") ? 3 : 0);
    auto *rigctl = new QLineEdit(ptt.value(QStringLiteral("rigctlAddress")).toString(), &dialog);

    auto *serial = new QComboBox(&dialog);
    serial->setEditable(true);
    serial->setInsertPolicy(QComboBox::NoInsert);
    auto loadSerialPorts = [this, serial]() {
        QString portsError;
        QJsonValue portsValue = m_backend->callSync(QStringLiteral("GetSerialPorts"), {}, &portsError, 5000);
        if (!portsError.isEmpty() || portsValue.toArray().isEmpty()) {
            portsValue = m_backend->callSync(QStringLiteral("RefreshSerialPorts"), {}, &portsError, 5000);
        }
        if (!portsError.isEmpty()) { appendLog(QStringLiteral("串口列表不可用：") + portsError); return; }
        const QString current = serial->currentText();
        serial->clear();
        for (const auto &value : portsValue.toArray()) serial->addItem(value.toString());
        serial->setCurrentText(current);
    };
    loadSerialPorts();
    serial->setCurrentText(ptt.value(QStringLiteral("serialPort")).toString());
    auto *refreshPorts = new QPushButton(QStringLiteral("刷新串口"), &dialog);
    auto *serialRow = new QWidget(&dialog);
    auto *serialRowLayout = new QHBoxLayout(serialRow);
    serialRowLayout->setContentsMargins(0, 0, 0, 0);
    serialRowLayout->addWidget(serial, 1);
    serialRowLayout->addWidget(refreshPorts);
    connect(refreshPorts, &QPushButton::clicked, &dialog, loadSerialPorts);

    auto *activeHigh = new QCheckBox(QStringLiteral("PTT 高电平有效"), &dialog);
    activeHigh->setChecked(ptt.value(QStringLiteral("activeHigh")).toBool(true));
    auto *lead = new QSpinBox(&dialog); lead->setRange(0, 10000); lead->setSuffix(QStringLiteral(" ms")); lead->setValue(settings.value(QStringLiteral("pttLeadMs")).toInt(300));
    auto *tail = new QSpinBox(&dialog); tail->setRange(0, 10000); tail->setSuffix(QStringLiteral(" ms")); tail->setValue(settings.value(QStringLiteral("pttTailMs")).toInt(250));

    form->addRow(QStringLiteral("本站呼号"), callsign);
    form->addRow(QStringLiteral("本站网格"), locator);
    form->addRow(QStringLiteral("界面语言（本地化迁移中）"), language);
    form->addRow(QStringLiteral("音频采样率"), sample);
    form->addRow(QStringLiteral("RX 音频输入"), rxDevice);
    form->addRow(QStringLiteral("TX 音频输出"), txDevice);
    form->addRow(QStringLiteral("PTT 控制方式"), pttMode);
    form->addRow(QStringLiteral("rigctld 地址"), rigctl);
    form->addRow(QStringLiteral("串口设备"), serialRow);
    form->addRow(QString(), activeHigh);
    form->addRow(QStringLiteral("PTT 发射前延时"), lead);
    form->addRow(QStringLiteral("PTT 发射后延时"), tail);
    layout->addLayout(form);

    auto collectSettings = [=]() mutable {
        QJsonObject updated = settings;
        updated.insert(QStringLiteral("callsign"), callsign->text().trimmed());
        updated.insert(QStringLiteral("locator"), locator->text().trimmed());
        updated.insert(QStringLiteral("language"), language->currentIndex() == 1 ? QStringLiteral("en") : language->currentIndex() == 2 ? QStringLiteral("ja") : QStringLiteral("zh-CN"));
        updated.insert(QStringLiteral("audioSampleRate"), sample->currentText().toInt());
        updated.insert(QStringLiteral("rxInputDevice"), rxDevice->currentData().toString());
        updated.insert(QStringLiteral("txOutputDevice"), txDevice->currentData().toString());
        QJsonObject updatedPTT = updated.value(QStringLiteral("ptt")).toObject();
        updatedPTT.insert(QStringLiteral("mode"), pttMode->currentIndex() == 1 ? QStringLiteral("hamlib") : pttMode->currentIndex() == 2 ? QStringLiteral("rts") : pttMode->currentIndex() == 3 ? QStringLiteral("dtr") : QStringLiteral("vox"));
        updatedPTT.insert(QStringLiteral("rigctlAddress"), rigctl->text().trimmed());
        updatedPTT.insert(QStringLiteral("serialPort"), serial->currentText().trimmed());
        updatedPTT.insert(QStringLiteral("activeHigh"), activeHigh->isChecked());
        updated.insert(QStringLiteral("ptt"), updatedPTT);
        updated.insert(QStringLiteral("pttLeadMs"), lead->value());
        updated.insert(QStringLiteral("pttTailMs"), tail->value());
        return updated;
    };

    auto *pttActions = new QHBoxLayout();
    auto *pttStart = new QPushButton(QStringLiteral("保存配置并测试 PTT"), &dialog);
    auto *pttStop = new QPushButton(QStringLiteral("停止 PTT 测试"), &dialog);
    pttActions->addWidget(pttStart);
    pttActions->addWidget(pttStop);
    pttActions->addStretch();
    layout->addLayout(pttActions);
    connect(pttStart, &QPushButton::clicked, &dialog, [this, collectSettings]() mutable {
        const QJsonObject updated = collectSettings();
        QString saveError;
        m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{updated}, &saveError, 15000);
        if (!saveError.isEmpty()) { showError(saveError); return; }
        m_settings = updated;
        m_backend->call(QStringLiteral("StartPTTTest"), {}, [this](bool ok, const QJsonValue &, const QString &pttError) {
            if (ok) appendLog(QStringLiteral("PTT 测试已启动")); else showError(pttError);
        });
    });
    connect(pttStop, &QPushButton::clicked, &dialog, [this](bool) {
        m_backend->call(QStringLiteral("StopPTTTest"), {}, [this](bool ok, const QJsonValue &, const QString &pttError) {
            if (ok) appendLog(QStringLiteral("PTT 测试已停止")); else showError(pttError);
        });
    });

    auto *buttons = new QDialogButtonBox(QDialogButtonBox::Save | QDialogButtonBox::Cancel, &dialog);
    layout->addWidget(buttons);
    connect(buttons, &QDialogButtonBox::accepted, &dialog, &QDialog::accept);
    connect(buttons, &QDialogButtonBox::rejected, &dialog, &QDialog::reject);
    if (dialog.exec() != QDialog::Accepted) return;

    const QJsonObject updated = collectSettings();
    QString saveError;
    m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{updated}, &saveError, 15000);
    if (!saveError.isEmpty()) { showError(saveError); return; }
    m_settings = updated;
    m_callsign->setText(callsign->text());
    m_locator->setText(locator->text());
    m_ax25SSID->setValue(updated.value(QStringLiteral("ax25Ssid")).toInt(m_ax25SSID->value()));
    appendLog(QStringLiteral("设备设置已保存"));
    refreshStatus();
}

void MainWindow::importGalleryImage() {
    const QString path=QFileDialog::getOpenFileName(this,QStringLiteral("导入图库图片"),QStandardPaths::writableLocation(QStandardPaths::PicturesLocation),QStringLiteral("Images (*.png *.jpg *.jpeg *.bmp *.webp)"));if(path.isEmpty())return;
    m_backend->call(QStringLiteral("ImportGalleryImage"),QJsonArray{path},[this](bool ok,const QJsonValue &value,const QString &error){if(!ok){showError(error);return;}const auto asset=value.toObject();appendLog(QStringLiteral("已导入图库：")+asset.value(QStringLiteral("name")).toString());refreshGallery();});
}
void MainWindow::exportAlbum() {
    const QString dir=QFileDialog::getExistingDirectory(this,QStringLiteral("导出相册到文件夹"),QStandardPaths::writableLocation(QStandardPaths::PicturesLocation));if(dir.isEmpty())return;
    m_backend->call(QStringLiteral("ExportAlbumToDirectoryPath"),QJsonArray{dir},[this](bool ok,const QJsonValue &value,const QString &error){if(!ok){showError(error);return;}QMessageBox::information(this,QStringLiteral("导出完成"),QStringLiteral("已导出 %1 条记录").arg(value.toInt()));});
}
void MainWindow::exportTemplatePack() {
    const QString path=QFileDialog::getSaveFileName(this,QStringLiteral("导出模板包"),QStringLiteral("satssdv-templates.zip"),QStringLiteral("Template packs (*.zip)"));if(path.isEmpty())return;
    m_backend->call(QStringLiteral("ExportTemplatePackTo"),QJsonArray{path},[this, path](bool ok,const QJsonValue&,const QString &error){if(ok)appendLog(QStringLiteral("模板包已导出：")+path);else showError(error);});
}
void MainWindow::importTemplatePack() {
    const QString path=QFileDialog::getOpenFileName(this,QStringLiteral("导入模板包"),QString(),QStringLiteral("Template packs (*.zip)"));if(path.isEmpty())return;
    m_backend->call(QStringLiteral("ImportTemplatePackFrom"),QJsonArray{path},[this](bool ok,const QJsonValue &value,const QString &error){if(!ok){showError(error);return;}refreshTemplates();QMessageBox::information(this,QStringLiteral("导入完成"),QStringLiteral("已导入 %1 个模板").arg(value.toInt()));});
}

void MainWindow::editSelectedTemplate() {
    const QString selectedId = m_template->currentData().toString();
    if (selectedId.isEmpty()) return;

    int templateIndex = -1;
    QJsonObject selected;
    for (int i = 0; i < m_templates.size(); ++i) {
        const QJsonObject object = m_templates.at(i).toObject();
        if (object.value(QStringLiteral("id")).toString() == selectedId) {
            templateIndex = i;
            selected = object;
            break;
        }
    }
    if (templateIndex < 0) return;

    refreshGallery();
    QJsonArray components = selected.value(QStringLiteral("components")).toArray();

    QImage background(640, 480, QImage::Format_ARGB32_Premultiplied);
    background.fill(QColor(QStringLiteral("#101820")));
    const QString backgroundId = m_settings.value(QStringLiteral("templateBackgroundAssetId")).toString();
    if (!backgroundId.isEmpty()) {
        QString backgroundError;
        const QString data = m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{backgroundId}, &backgroundError, 10000).toString();
        if (backgroundError.isEmpty()) {
            const QImage source = imageFromDataUrl(data);
            if (!source.isNull()) {
                QPainter painter(&background);
                const qreal factor = qMax(background.width() / qreal(source.width()), background.height() / qreal(source.height()));
                const QSizeF scaled(source.width() * factor, source.height() * factor);
                painter.drawImage(QRectF((background.width() - scaled.width()) / 2.0,
                                         (background.height() - scaled.height()) / 2.0,
                                         scaled.width(), scaled.height()), source);
            }
        }
    }

    QHash<QString, QImage> componentImages;
    QHash<QString, QImage> galleryImageCache;
    QSet<QString> loadedLastRx;
    for (const QJsonValue &value : components) {
        const QJsonObject component = value.toObject();
        const QString id = component.value(QStringLiteral("id")).toString();
        const QString type = component.value(QStringLiteral("type")).toString();
        if (type == QStringLiteral("image")) {
            const QString assetId = component.value(QStringLiteral("assetId")).toString();
            if (!assetId.isEmpty() && !galleryImageCache.contains(assetId)) {
                QString imageError;
                const QString data = m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{assetId}, &imageError, 10000).toString();
                if (imageError.isEmpty()) galleryImageCache.insert(assetId, imageFromDataUrl(data));
            }
            componentImages.insert(id, galleryImageCache.value(assetId));
        } else if (type == QStringLiteral("last-rx-image")) {
            if (!loadedLastRx.contains(QStringLiteral("last-rx"))) {
                QString imageError;
                const QString data = m_backend->callSync(QStringLiteral("GetLatestRXImageData"), {}, &imageError, 10000).toString();
                if (imageError.isEmpty()) galleryImageCache.insert(QStringLiteral("last-rx"), imageFromDataUrl(data));
                loadedLastRx.insert(QStringLiteral("last-rx"));
            }
            componentImages.insert(id, galleryImageCache.value(QStringLiteral("last-rx")));
        }
    }

    QDialog dialog(this);
    dialog.setWindowTitle(QStringLiteral("模板可视化编辑器"));
    dialog.resize(1260, 820);
    auto *root = new QVBoxLayout(&dialog);
    auto *nameRow = new QHBoxLayout();
    nameRow->addWidget(new QLabel(QStringLiteral("模板名称"), &dialog));
    auto *nameEdit = new QLineEdit(selected.value(QStringLiteral("name")).toString(), &dialog);
    nameEdit->setMaxLength(48);
    nameRow->addWidget(nameEdit, 1);
    root->addLayout(nameRow);

    auto *toolbar = new QHBoxLayout();
    auto *addText = new QPushButton(QStringLiteral("＋ 文字组件"), &dialog);
    auto *addImage = new QPushButton(QStringLiteral("＋ 图库图像"), &dialog);
    auto *addLastRx = new QPushButton(QStringLiteral("＋ 上次接收图像"), &dialog);
    auto *moveLayerUp = new QPushButton(QStringLiteral("上移图层"), &dialog);
    auto *moveLayerDown = new QPushButton(QStringLiteral("下移图层"), &dialog);
    auto *snapGrid = new QCheckBox(QStringLiteral("5% 网格吸附"), &dialog);
    auto *deleteComponent = new QPushButton(QStringLiteral("删除选中组件"), &dialog);
    deleteComponent->setProperty("role", "danger");
    toolbar->addWidget(addText);
    toolbar->addWidget(addImage);
    toolbar->addWidget(addLastRx);
    toolbar->addWidget(moveLayerUp);
    toolbar->addWidget(moveLayerDown);
    toolbar->addWidget(snapGrid);
    toolbar->addWidget(deleteComponent);
    toolbar->addStretch();
    root->addLayout(toolbar);

    auto *split = new QSplitter(Qt::Horizontal, &dialog);
    auto *canvas = new TemplateCanvas(split);
    canvas->setDocument(components, background, componentImages,
                        [this](const QString &text) { return resolveTemplateText(text); });

    auto *properties = new QWidget(split);
    auto *propertiesLayout = new QVBoxLayout(properties);
    propertiesLayout->setContentsMargins(6, 0, 0, 0);
    propertiesLayout->addWidget(new QLabel(QStringLiteral("文字组件属性"), properties));
    auto *table = new QTableWidget(properties);
    table->setColumnCount(5);
    table->setHorizontalHeaderLabels({QStringLiteral("文字内容"), QStringLiteral("X %"), QStringLiteral("Y %"), QStringLiteral("字号"), QStringLiteral("颜色")});
    table->horizontalHeader()->setSectionResizeMode(0, QHeaderView::Stretch);
    for (int column = 1; column < 5; ++column) table->horizontalHeader()->setSectionResizeMode(column, QHeaderView::ResizeToContents);
    table->setSelectionBehavior(QAbstractItemView::SelectRows);
    table->setAlternatingRowColors(true);
    propertiesLayout->addWidget(table, 1);
    propertiesLayout->addWidget(new QLabel(QStringLiteral("画布操作：拖动组件移动；拖动右下角控制点调整文字字号或图像比例；选中后按 Delete 删除。"), properties));
    split->addWidget(canvas);
    split->addWidget(properties);
    split->setStretchFactor(0, 3);
    split->setStretchFactor(1, 2);
    root->addWidget(split, 1);

    QVector<int> componentIndexes;
    int knownComponentCount = components.size();
    auto rebuildProperties = [&]() {
        const QSignalBlocker blocker(table);
        knownComponentCount = components.size();
        componentIndexes.clear();
        table->setRowCount(0);
        for (int index = 0; index < components.size(); ++index) {
            const QJsonObject component = components.at(index).toObject();
            if (component.value(QStringLiteral("type")).toString() != QStringLiteral("text")) continue;
            const int row = table->rowCount();
            table->insertRow(row);
            componentIndexes.push_back(index);
            table->setItem(row, 0, new QTableWidgetItem(component.value(QStringLiteral("text")).toString()));
            table->setItem(row, 1, new QTableWidgetItem(QString::number(component.value(QStringLiteral("x")).toDouble())));
            table->setItem(row, 2, new QTableWidgetItem(QString::number(component.value(QStringLiteral("y")).toDouble())));
            table->setItem(row, 3, new QTableWidgetItem(QString::number(component.value(QStringLiteral("fontSize")).toDouble(24.0))));
            table->setItem(row, 4, new QTableWidgetItem(valueOr(component, QStringLiteral("color"), QStringLiteral("#ffffff"))));
        }
    };
    rebuildProperties();

    canvas->setComponentsChangedCallback([&](const QJsonArray &updated) {
        bool structureOrOrderChanged = updated.size() != knownComponentCount;
        if (!structureOrOrderChanged && updated.size() == components.size()) {
            for (int i = 0; i < updated.size(); ++i) {
                const QString oldID = components.at(i).toObject().value(QStringLiteral("id")).toString();
                const QString newID = updated.at(i).toObject().value(QStringLiteral("id")).toString();
                if (oldID != newID) { structureOrOrderChanged = true; break; }
            }
        }
        components = updated;
        if (structureOrOrderChanged) {
            rebuildProperties();
            return;
        }
        const QSignalBlocker blocker(table);
        for (int row = 0; row < componentIndexes.size(); ++row) {
            const int index = componentIndexes[row];
            if (index < 0 || index >= components.size()) continue;
            const QJsonObject component = components.at(index).toObject();
            table->item(row, 1)->setText(QString::number(component.value(QStringLiteral("x")).toDouble(), 'f', 2));
            table->item(row, 2)->setText(QString::number(component.value(QStringLiteral("y")).toDouble(), 'f', 2));
            table->item(row, 3)->setText(QString::number(component.value(QStringLiteral("fontSize")).toDouble(24.0), 'f', 1));
        }
    });
    canvas->setSelectionChangedCallback([&](int componentIndex) {
        const int row = componentIndexes.indexOf(componentIndex);
        if (row >= 0) table->selectRow(row);
        else table->clearSelection();
    });
    connect(moveLayerUp, &QPushButton::clicked, canvas, [canvas] { canvas->moveSelectedLayerUp(); });
    connect(moveLayerDown, &QPushButton::clicked, canvas, [canvas] { canvas->moveSelectedLayerDown(); });
    connect(snapGrid, &QCheckBox::toggled, canvas, [canvas](bool enabled) { canvas->setSnapToGrid(enabled); });
    connect(table, &QTableWidget::currentCellChanged, &dialog,
            [canvas, &componentIndexes](int row, int, int, int) {
        if (row >= 0 && row < componentIndexes.size()) canvas->setSelectedIndex(componentIndexes[row]);
    });
    connect(table, &QTableWidget::itemChanged, &dialog, [&](QTableWidgetItem *item) {
        if (!item || item->row() < 0 || item->row() >= componentIndexes.size()) return;
        const int componentIndex = componentIndexes[item->row()];
        if (componentIndex < 0 || componentIndex >= components.size()) return;
        QJsonObject component = components.at(componentIndex).toObject();
        const int column = item->column();
        if (column == 0) {
            component.insert(QStringLiteral("text"), item->text());
        } else if (column >= 1 && column <= 3) {
            bool ok = false;
            const double value = item->text().toDouble(&ok);
            const double minimum = column == 3 ? 6.0 : 0.0;
            const double maximum = column == 3 ? 160.0 : 100.0;
            if (!ok || !std::isfinite(value) || value < minimum || value > maximum) {
                const QSignalBlocker blocker(table);
                const QString previous = column == 1 ? QString::number(component.value(QStringLiteral("x")).toDouble())
                    : column == 2 ? QString::number(component.value(QStringLiteral("y")).toDouble())
                    : QString::number(component.value(QStringLiteral("fontSize")).toDouble(24.0));
                item->setText(previous);
                return;
            }
            component.insert(column == 1 ? QStringLiteral("x") : column == 2 ? QStringLiteral("y") : QStringLiteral("fontSize"), value);
        } else if (column == 4) {
            const QColor color(item->text().trimmed());
            if (!color.isValid()) {
                const QSignalBlocker blocker(table);
                item->setText(component.value(QStringLiteral("color")).toString(QStringLiteral("#ffffff")));
                return;
            }
            component.insert(QStringLiteral("color"), color.name(QColor::HexRgb));
        }
        components.replace(componentIndex, component);
        canvas->setDocument(components, background, componentImages,
                            [this](const QString &text) { return resolveTemplateText(text); });
    });

    auto rebuildCanvasAndProperties = [&]() {
        rebuildProperties();
        canvas->setDocument(components, background, componentImages,
                            [this](const QString &text) { return resolveTemplateText(text); });
    };
    connect(addText, &QPushButton::clicked, &dialog, [&]() {
        QJsonObject component;
        component.insert(QStringLiteral("id"), QUuid::createUuid().toString(QUuid::WithoutBraces));
        component.insert(QStringLiteral("type"), QStringLiteral("text"));
        component.insert(QStringLiteral("text"), QStringLiteral("新文本"));
        component.insert(QStringLiteral("x"), 8.0);
        component.insert(QStringLiteral("y"), 45.0);
        component.insert(QStringLiteral("width"), 80.0);
        component.insert(QStringLiteral("height"), 16.0);
        component.insert(QStringLiteral("fontSize"), 28.0);
        component.insert(QStringLiteral("bold"), true);
        component.insert(QStringLiteral("color"), QStringLiteral("#ffffff"));
        component.insert(QStringLiteral("visible"), true);
        component.insert(QStringLiteral("outline"), true);
        component.insert(QStringLiteral("outlineWidth"), 2.0);
        component.insert(QStringLiteral("outlineColor"), QStringLiteral("#101820"));
        components.append(component);
        rebuildCanvasAndProperties();
        canvas->setSelectedIndex(components.size() - 1);
    });
    connect(addImage, &QPushButton::clicked, &dialog, [&]() {
        refreshGallery();
        QStringList labels;
        QStringList ids;
        for (const QJsonValue &value : m_gallery) {
            const QJsonObject asset = value.toObject();
            labels << valueOr(asset, QStringLiteral("name"), asset.value(QStringLiteral("id")).toString());
            ids << asset.value(QStringLiteral("id")).toString();
        }
        if (labels.isEmpty()) {
            QMessageBox::information(&dialog, QStringLiteral("图库为空"), QStringLiteral("请先在图像图库页面导入图片，再添加图像组件。"));
            return;
        }
        bool ok = false;
        const QString choice = QInputDialog::getItem(&dialog, QStringLiteral("添加图像组件"), QStringLiteral("选择图库图像"), labels, 0, false, &ok);
        if (!ok) return;
        const int selectedAsset = labels.indexOf(choice);
        if (selectedAsset < 0) return;
        QJsonObject component;
        const QString componentId = QUuid::createUuid().toString(QUuid::WithoutBraces);
        component.insert(QStringLiteral("id"), componentId);
        component.insert(QStringLiteral("type"), QStringLiteral("image"));
        component.insert(QStringLiteral("assetId"), ids[selectedAsset]);
        component.insert(QStringLiteral("x"), 32.0);
        component.insert(QStringLiteral("y"), 28.0);
        component.insert(QStringLiteral("scale"), 35.0);
        component.insert(QStringLiteral("visible"), true);
        component.insert(QStringLiteral("outline"), false);
        QString imageError;
        const QString imageData = m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{ids[selectedAsset]}, &imageError, 10000).toString();
        if (!imageError.isEmpty()) { showError(imageError); return; }
        componentImages.insert(componentId, imageFromDataUrl(imageData));
        components.append(component);
        rebuildCanvasAndProperties();
        canvas->setSelectedIndex(components.size() - 1);
    });
    connect(addLastRx, &QPushButton::clicked, &dialog, [&]() {
        QJsonObject component;
        const QString componentId = QUuid::createUuid().toString(QUuid::WithoutBraces);
        component.insert(QStringLiteral("id"), componentId);
        component.insert(QStringLiteral("type"), QStringLiteral("last-rx-image"));
        component.insert(QStringLiteral("x"), 58.0);
        component.insert(QStringLiteral("y"), 30.0);
        component.insert(QStringLiteral("scale"), 35.0);
        component.insert(QStringLiteral("visible"), true);
        component.insert(QStringLiteral("outline"), false);
        QString imageError;
        const QString imageData = m_backend->callSync(QStringLiteral("GetLatestRXImageData"), {}, &imageError, 10000).toString();
        if (imageError.isEmpty()) componentImages.insert(componentId, imageFromDataUrl(imageData));
        components.append(component);
        rebuildCanvasAndProperties();
        canvas->setSelectedIndex(components.size() - 1);
    });
    connect(deleteComponent, &QPushButton::clicked, &dialog, [&]() {
        canvas->removeSelectedComponent();
        components = canvas->components();
        rebuildProperties();
    });

    auto *buttons = new QDialogButtonBox(QDialogButtonBox::Save | QDialogButtonBox::Cancel, &dialog);
    root->addWidget(buttons);
    connect(buttons, &QDialogButtonBox::accepted, &dialog, &QDialog::accept);
    connect(buttons, &QDialogButtonBox::rejected, &dialog, &QDialog::reject);
    if (dialog.exec() != QDialog::Accepted) return;

    components = canvas->components();
    for (int row = 0; row < componentIndexes.size(); ++row) {
        const int index = componentIndexes[row];
        if (index < 0 || index >= components.size()) continue;
        QJsonObject component = components.at(index).toObject();
        bool okX = false, okY = false, okSize = false;
        const double x = table->item(row, 1)->text().toDouble(&okX);
        const double y = table->item(row, 2)->text().toDouble(&okY);
        const double size = table->item(row, 3)->text().toDouble(&okSize);
        if (!okX || !okY || !okSize || x < 0 || x > 100 || y < 0 || y > 100 || size < 6 || size > 160) {
            showError(QStringLiteral("模板位置必须为 0–100%，字号必须为 6–160 px。"));
            return;
        }
        component.insert(QStringLiteral("text"), table->item(row, 0)->text());
        component.insert(QStringLiteral("x"), x);
        component.insert(QStringLiteral("y"), y);
        component.insert(QStringLiteral("fontSize"), size);
        component.insert(QStringLiteral("color"), table->item(row, 4)->text().trimmed());
        components.replace(index, component);
    }

    const QString name = nameEdit->text().trimmed();
    if (name.isEmpty()) {
        showError(QStringLiteral("模板名称不能为空。"));
        return;
    }
    selected.insert(QStringLiteral("name"), name);
    selected.insert(QStringLiteral("components"), components);
    m_templates.replace(templateIndex, selected);
    m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{m_templates}, [this](bool ok, const QJsonValue &, const QString &error) {
        if (!ok) { showError(error); return; }
        appendLog(QStringLiteral("模板布局已保存"));
        refreshTemplates();
        const QImage image = renderTemplate();
        if (!image.isNull()) m_txPreview->setSourceImage(image);
    });
}

void MainWindow::selectTemplateBackground() {
    refreshGallery();QStringList labels;QStringList ids;for(const auto &value:m_gallery){const auto asset=value.toObject();labels<<valueOr(asset,QStringLiteral("name"),asset.value(QStringLiteral("id")).toString());ids<<asset.value(QStringLiteral("id")).toString();}
    if(labels.isEmpty()){if(QMessageBox::question(this,QStringLiteral("模板背景"),QStringLiteral("图库为空，是否先导入图片？"))==QMessageBox::Yes)importGalleryImage();return;}
    bool ok=false;const QString choice=QInputDialog::getItem(this,QStringLiteral("选择模板背景"),QStringLiteral("背景图片"),labels,0,false,&ok);if(!ok)return;const int index=labels.indexOf(choice);if(index<0)return;
    m_settings.insert(QStringLiteral("templateBackgroundAssetId"),ids[index]);saveSettings(m_settings);appendLog(QStringLiteral("模板背景已更新"));QImage image=renderTemplate();if(!image.isNull())m_txPreview->setSourceImage(image);
}

void MainWindow::showImage(const QString &dataUrl, ImagePreviewLabel *target) { const QImage image = imageFromDataUrl(dataUrl); if (!image.isNull()) target->setSourceImage(image); }
void MainWindow::appendLog(const QString &text){if(m_log){m_log->append(QStringLiteral("[%1] %2").arg(QDateTime::currentDateTime().toString(QStringLiteral("HH:mm:ss.zzz")),text));}}
void MainWindow::showError(const QString &message) {
    appendLog(QStringLiteral("错误：") + message);
    statusBar()->showMessage(message, 10000);
    if (m_audioStatus) m_audioStatus->setToolTip(message);
}
