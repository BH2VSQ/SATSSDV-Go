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

MainWindow::MainWindow(BackendClient *backend, QWidget *parent)
    : QMainWindow(parent), m_backend(backend) {
    buildUi();
    loadInitialState();
    connect(&m_eventTimer, &QTimer::timeout, this, [this] { pollEvents(); });
    connect(&m_statusTimer, &QTimer::timeout, this, [this] { refreshStatus(); });
    m_eventTimer.start(80);
    m_statusTimer.start(2000);
    refreshStatus();
}

void MainWindow::stopBackgroundPolling() {
    m_eventTimer.stop();
    m_statusTimer.stop();
}

void MainWindow::buildUi() {
    setWindowTitle(QStringLiteral("SATSSDV-Go · Native Qt 6"));
    setMinimumSize(1080, 720);
    resize(1440, 900);
    setStyleSheet(QStringLiteral(
        "QMainWindow,QWidget { background:#101820; color:#e6edf3; font-family:'Segoe UI','Microsoft YaHei UI','Noto Sans CJK SC'; font-size:10pt; }"
        "QGroupBox { border:1px solid #344553; border-radius:8px; margin-top:12px; padding:12px; font-weight:600; }"
        "QGroupBox::title { subcontrol-origin:margin; left:12px; padding:0 5px; color:#72d2e6; }"
        "QPushButton { background:#245a70; border:1px solid #397d94; border-radius:6px; padding:8px 14px; min-height:18px; }"
        "QPushButton:hover { background:#2e718a; } QPushButton:disabled { background:#303c45; color:#87939c; }"
        "QPushButton[role='danger'] { background:#7b3540; border-color:#a64a58; }"
        "QComboBox,QLineEdit,QSpinBox { background:#1b2833; border:1px solid #405564; border-radius:5px; padding:6px; min-height:20px; }"
        "QTabWidget::pane { border:1px solid #344553; border-radius:8px; } QTabBar::tab { background:#1b2833; padding:10px 18px; margin-right:3px; border-top-left-radius:6px; border-top-right-radius:6px; } QTabBar::tab:selected { background:#245a70; }"
        "QHeaderView::section { background:#1b2833; color:#9edce9; padding:7px; border:0; }"
        "QTableWidget { background:#16222c; alternate-background-color:#1a2934; gridline-color:#344553; border:1px solid #344553; }"
        "QTextEdit { background:#0b1218; border:1px solid #344553; border-radius:6px; }"
    ));

    auto *central = new QWidget(this);
    auto *root = new QVBoxLayout(central);
    root->setContentsMargins(16, 14, 16, 12);
    root->setSpacing(12);

    auto *header = new QHBoxLayout();
    auto *brand = new QLabel(QStringLiteral("SATSSDV-Go"), central);
    QFont brandFont = brand->font(); brandFont.setPointSize(20); brandFont.setBold(true); brand->setFont(brandFont);
    auto *subtitle = new QLabel(QStringLiteral("原生桌面无线图像收发终端 · Qt 6 Widgets"), central);
    subtitle->setStyleSheet(QStringLiteral("color:#91a7b6;"));
    auto *brandStack = new QVBoxLayout(); brandStack->addWidget(brand); brandStack->addWidget(subtitle);
    header->addLayout(brandStack); header->addStretch();
    m_audioStatus = new QLabel(QStringLiteral("音频：正在连接"), central);
    m_audioStatus->setStyleSheet(QStringLiteral("background:#1b2833; border:1px solid #344553; padding:8px 12px; border-radius:6px;"));
    auto *settingsButton = new QPushButton(QStringLiteral("设备设置"), central);
    connect(settingsButton, &QPushButton::clicked, this, [this] { openSettings(); });
    header->addWidget(m_audioStatus); header->addWidget(settingsButton);
    root->addLayout(header);

    auto *tabs = new QTabWidget(central);
    auto *workbench = new QWidget(tabs);
    auto *workLayout = new QVBoxLayout(workbench);
    workLayout->setContentsMargins(10, 10, 10, 10);
    auto *split = new QSplitter(Qt::Horizontal, workbench);

    auto *left = new QWidget(split);
    auto *leftLayout = new QVBoxLayout(left);
    auto *rxBox = new QGroupBox(QStringLiteral("接收 RX"), left);
    auto *rxLayout = new QVBoxLayout(rxBox);
    auto *rxProfileRow = new QHBoxLayout();
    rxProfileRow->addWidget(new QLabel(QStringLiteral("接收模式"), rxBox));
    m_rxProfile = makeCombo(rxBox, {}); m_rxProfile->setMinimumWidth(220); rxProfileRow->addWidget(m_rxProfile, 1);
    rxLayout->addLayout(rxProfileRow);
    auto *rxButtons = new QHBoxLayout();
    auto *rxStart = new QPushButton(QStringLiteral("启动接收"), rxBox);
    auto *rxStop = new QPushButton(QStringLiteral("停止接收"), rxBox); rxStop->setProperty("role", "danger");
    rxButtons->addWidget(rxStart); rxButtons->addWidget(rxStop); rxLayout->addLayout(rxButtons);
    m_rxStatus = new QLabel(QStringLiteral("RX 待机"), rxBox); m_rxStatus->setWordWrap(true); rxLayout->addWidget(m_rxStatus);
    m_rxPreview = makePreview(rxBox); rxLayout->addWidget(m_rxPreview, 1);
    connect(rxStart, &QPushButton::clicked, this, [this] { startRX(); });
    connect(rxStop, &QPushButton::clicked, this, [this] { stopRX(); });
    leftLayout->addWidget(rxBox, 1);

    auto *txBox = new QGroupBox(QStringLiteral("发射 TX"), left);
    auto *txLayout = new QVBoxLayout(txBox);
    auto *profileRow = new QHBoxLayout(); profileRow->addWidget(new QLabel(QStringLiteral("发射模式"), txBox));
    m_txProfile = makeCombo(txBox, {}); m_txProfile->setMinimumWidth(220); profileRow->addWidget(m_txProfile, 1); txLayout->addLayout(profileRow);
    auto *qualityRow = new QHBoxLayout(); qualityRow->addWidget(new QLabel(QStringLiteral("图像分辨率"), txBox));
    m_quality = makeCombo(txBox, {QStringLiteral("低 · 320×240"), QStringLiteral("中 · 448×336"), QStringLiteral("高 · 640×480")});
    qualityRow->addWidget(m_quality, 1); txLayout->addLayout(qualityRow);
    auto *templateRow = new QHBoxLayout(); templateRow->addWidget(new QLabel(QStringLiteral("模板"), txBox));
    m_template = makeCombo(txBox, {}); templateRow->addWidget(m_template, 1);
    auto *templateEdit = new QPushButton(QStringLiteral("编辑模板"), txBox); templateEdit->setMaximumWidth(100); templateRow->addWidget(templateEdit);
    auto *templateBackground = new QPushButton(QStringLiteral("背景图"), txBox); templateBackground->setMaximumWidth(80); templateRow->addWidget(templateBackground);
    auto *templateExport = new QPushButton(QStringLiteral("模板包"), txBox); templateExport->setMaximumWidth(80); templateRow->addWidget(templateExport); txLayout->addLayout(templateRow);
    connect(templateEdit, &QPushButton::clicked, this, [this] { editSelectedTemplate(); });
    connect(templateBackground, &QPushButton::clicked, this, [this] { selectTemplateBackground(); });
    connect(templateExport, &QPushButton::clicked, this, [this] {
        QMenu menu(this); auto *imp = menu.addAction(QStringLiteral("导入模板包")); auto *exp = menu.addAction(QStringLiteral("导出模板包"));
        QAction *chosen = menu.exec(QCursor::pos()); if (chosen == imp) importTemplatePack(); else if (chosen == exp) exportTemplatePack();
    });
    auto *qso = new QFormLayout();
    m_callsign = new QLineEdit(txBox); m_hisCall = new QLineEdit(QStringLiteral("CQ"), txBox);
    m_locator = new QLineEdit(txBox); m_report = new QLineEdit(QStringLiteral("59"), txBox);
    m_qth = new QLineEdit(txBox); m_message = new QLineEdit(txBox);
    m_ax25To = new QLineEdit(QStringLiteral("CQ"), txBox);
    m_ax25SSID = new QSpinBox(txBox); m_ax25SSID->setRange(0, 15); m_ax25SSID->setValue(0);
    qso->addRow(QStringLiteral("本站呼号"), m_callsign); qso->addRow(QStringLiteral("对方呼号"), m_hisCall);
    qso->addRow(QStringLiteral("本站网格"), m_locator); qso->addRow(QStringLiteral("信号报告"), m_report);
    qso->addRow(QStringLiteral("QTH"), m_qth); qso->addRow(QStringLiteral("附加信息"), m_message);
    qso->addRow(QStringLiteral("AX.25 To"), m_ax25To); qso->addRow(QStringLiteral("AX.25 Source SSID"), m_ax25SSID); txLayout->addLayout(qso);
    auto *repeatRow = new QHBoxLayout(); m_repeat = new QCheckBox(QStringLiteral("循环重复发射"), txBox);
    m_repeatInterval = new QSpinBox(txBox); m_repeatInterval->setRange(0, 86400); m_repeatInterval->setValue(30); m_repeatInterval->setSuffix(QStringLiteral(" 秒间隔"));
    repeatRow->addWidget(m_repeat); repeatRow->addWidget(m_repeatInterval); repeatRow->addStretch(); txLayout->addLayout(repeatRow);
    m_txPreview = makePreview(txBox); txLayout->addWidget(m_txPreview, 1);
    auto *txButtons = new QHBoxLayout(); auto *send = new QPushButton(QStringLiteral("发送当前图像"), txBox);
    auto *tone = new QPushButton(QStringLiteral("1500 Hz 测试音"), txBox); auto *stop = new QPushButton(QStringLiteral("停止 TX"), txBox); stop->setProperty("role", "danger");
    txButtons->addWidget(send); txButtons->addWidget(tone); txButtons->addWidget(stop); txLayout->addLayout(txButtons);
    m_txProgress = new QProgressBar(txBox); m_txProgress->setRange(0, 1000); m_txProgress->setValue(0); txLayout->addWidget(m_txProgress);
    connect(send, &QPushButton::clicked, this, [this] { startTX(); });
    connect(tone, &QPushButton::clicked, this, [this] { QString error; m_backend->call(QStringLiteral("Start1500HzTestTone"), {}, [this](bool ok,const QJsonValue&,const QString &e){ if(ok) appendLog(QStringLiteral("已启动 1500 Hz 测试音")); else showError(e); }); });
    connect(stop, &QPushButton::clicked, this, [this] { m_backend->call(QStringLiteral("StopTX"), {}, [this](bool,const QJsonValue&,const QString&){appendLog(QStringLiteral("已请求停止发射"));}); });
    leftLayout->addWidget(txBox, 2);

    auto *right = new QWidget(split); auto *rightLayout = new QVBoxLayout(right);
    auto *spectrumBox = new QGroupBox(QStringLiteral("实时频谱 / 瀑布数据"), right); auto *spectrumLayout = new QVBoxLayout(spectrumBox);
    m_spectrumStatus = new QLabel(QStringLiteral("等待音频频谱事件…"), spectrumBox); m_spectrumStatus->setWordWrap(true); spectrumLayout->addWidget(m_spectrumStatus);
    auto *spectrumInfo = new QLabel(QStringLiteral("界面使用原生 Qt 绘制；频谱事件由 Go 后端按约 15 Hz 节流。"), spectrumBox); spectrumInfo->setStyleSheet(QStringLiteral("color:#91a7b6;")); spectrumLayout->addWidget(spectrumInfo);
    m_spectrum = new SpectrumWaterfallWidget(spectrumBox); m_spectrum->setStyleSheet(QStringLiteral("border:1px solid #344553; border-radius:6px;")); spectrumLayout->addWidget(m_spectrum, 1);
    rightLayout->addWidget(spectrumBox, 1);
    auto *logBox = new QGroupBox(QStringLiteral("运行日志"), right); auto *logLayout = new QVBoxLayout(logBox);
    m_log = new QTextEdit(logBox); m_log->setReadOnly(true); m_log->setMinimumHeight(180); logLayout->addWidget(m_log); rightLayout->addWidget(logBox, 1);
    split->addWidget(left); split->addWidget(right); split->setStretchFactor(0, 3); split->setStretchFactor(1, 2);
    workLayout->addWidget(split, 1);
    tabs->addTab(workbench, QStringLiteral("收发工作台"));

    auto *albumPage = new QWidget(tabs); auto *albumLayout = new QVBoxLayout(albumPage);
    auto *albumToolbar = new QHBoxLayout(); auto *albumHint = new QLabel(QStringLiteral("接收与发送的图像历史记录"), albumPage); albumToolbar->addWidget(albumHint); albumToolbar->addStretch();
    auto *refresh = new QPushButton(QStringLiteral("刷新"), albumPage); auto *exportAll = new QPushButton(QStringLiteral("导出全部到文件夹"), albumPage); auto *importImage = new QPushButton(QStringLiteral("导入图库图片"), albumPage);
    albumToolbar->addWidget(refresh); albumToolbar->addWidget(importImage); albumToolbar->addWidget(exportAll); albumLayout->addLayout(albumToolbar);
    connect(refresh, &QPushButton::clicked, this, [this] { refreshAlbum(); refreshGallery(); });
    connect(exportAll, &QPushButton::clicked, this, [this] { exportAlbum(); });
    connect(importImage, &QPushButton::clicked, this, [this] { importGalleryImage(); });
    m_albumTable = new QTableWidget(albumPage); m_albumTable->setColumnCount(7);
    m_albumTable->setHorizontalHeaderLabels({QStringLiteral("类型"),QStringLiteral("时间"),QStringLiteral("呼号"),QStringLiteral("模式"),QStringLiteral("图像 ID"),QStringLiteral("进度"),QStringLiteral("状态")});
    m_albumTable->horizontalHeader()->setSectionResizeMode(QHeaderView::Stretch); m_albumTable->setSelectionBehavior(QAbstractItemView::SelectRows); m_albumTable->setEditTriggers(QAbstractItemView::NoEditTriggers); m_albumTable->setAlternatingRowColors(true); m_albumTable->setContextMenuPolicy(Qt::CustomContextMenu);
    connect(m_albumTable, &QTableWidget::cellDoubleClicked, this, [this](int row, int) {
        if(row<0 || row>=m_album.size()) return; const QString id=m_album.at(row).toObject().value(QStringLiteral("id")).toString(); QString error;
        const QString data=m_backend->callSync(QStringLiteral("GetAlbumImageData"),QJsonArray{id},&error,15000).toString(); if(!error.isEmpty()){showError(error);return;}
        const QImage image=imageFromDataUrl(data); if(image.isNull()){showError(QStringLiteral("无法读取相册图像"));return;}
        QDialog preview(this); preview.setWindowTitle(QStringLiteral("图像预览")); auto *layout=new QVBoxLayout(&preview); auto *label=new QLabel(&preview);label->setAlignment(Qt::AlignCenter);label->setPixmap(QPixmap::fromImage(image).scaled(1000,760,Qt::KeepAspectRatio,Qt::SmoothTransformation));layout->addWidget(label);preview.resize(qMin(1100,image.width()+40),qMin(850,image.height()+90));preview.exec();
    });
    connect(m_albumTable, &QWidget::customContextMenuRequested, this, [this](const QPoint &point) {
        const int row=m_albumTable->rowAt(point.y()); if(row<0 || row>=m_album.size())return;const QString id=m_album.at(row).toObject().value(QStringLiteral("id")).toString();
        QMenu menu(this); QAction *exportAction=menu.addAction(QStringLiteral("导出此图像…")); QAction *deleteAction=menu.addAction(QStringLiteral("删除记录")); QAction *selected=menu.exec(m_albumTable->viewport()->mapToGlobal(point));
        if(selected==exportAction){const QString dir=QFileDialog::getExistingDirectory(this,QStringLiteral("导出图像"),QStandardPaths::writableLocation(QStandardPaths::PicturesLocation));if(dir.isEmpty())return;m_backend->call(QStringLiteral("ExportAlbumRecordToDirectory"),QJsonArray{id,dir},[this](bool ok,const QJsonValue&,const QString &error){if(ok)appendLog(QStringLiteral("相册图像已导出"));else showError(error);});}
        else if(selected==deleteAction){if(QMessageBox::question(this,QStringLiteral("删除图像记录"),QStringLiteral("确定删除所选相册记录吗？"))!=QMessageBox::Yes)return;m_backend->call(QStringLiteral("DeleteAlbumRecord"),QJsonArray{id},[this](bool ok,const QJsonValue&,const QString &error){if(ok)refreshAlbum();else showError(error);});}
    });
    albumLayout->addWidget(m_albumTable, 1);
    tabs->addTab(albumPage, QStringLiteral("图像相册"));
    root->addWidget(tabs, 1);
    setCentralWidget(central);

    auto *fileMenu = menuBar()->addMenu(QStringLiteral("文件"));
    auto *importPack = fileMenu->addAction(QStringLiteral("导入模板包…")); connect(importPack,&QAction::triggered,this,[this]{importTemplatePack();});
    auto *exportPack = fileMenu->addAction(QStringLiteral("导出模板包…")); connect(exportPack,&QAction::triggered,this,[this]{exportTemplatePack();});
    auto *settingsAction = fileMenu->addAction(QStringLiteral("设备设置…")); connect(settingsAction,&QAction::triggered,this,[this]{openSettings();});
    auto *quit = fileMenu->addAction(QStringLiteral("退出")); connect(quit,&QAction::triggered,qApp,&QApplication::quit);
    statusBar()->showMessage(QStringLiteral("Go 后端与 Qt 原生界面通过本地 RPC 通信"));
}

void MainWindow::loadInitialState() {
    QString error;
    const auto profiles = m_backend->callSync(QStringLiteral("GetProfiles"), {}, &error);
    if (!error.isEmpty()) { showError(error); return; }
    m_profiles = profiles.toArray();
    for (const auto &value : m_profiles) {
        const auto profile = value.toObject();
        const QString label = valueOr(profile, QStringLiteral("name"), profile.value(QStringLiteral("id")).toString());
        const QString id = profile.value(QStringLiteral("id")).toString();
        m_rxProfile->addItem(label, id); m_txProfile->addItem(label, id);
    }
    const auto settings = m_backend->callSync(QStringLiteral("GetSettings"), {}, &error).toObject();
    if (!error.isEmpty()) { showError(error); return; }
    m_settings = settings;
    m_callsign->setText(valueOr(settings, QStringLiteral("callsign"), QStringLiteral("N0CALL")));
    m_locator->setText(valueOr(settings, QStringLiteral("locator"), QStringLiteral("JJ00")));
    m_ax25SSID->setValue(settings.value(QStringLiteral("ax25Ssid")).toInt(0));
    const QString rx = valueOr(settings, QStringLiteral("rxProfile"));
    const QString tx = valueOr(settings, QStringLiteral("txProfile"));
    int idx = m_rxProfile->findData(rx); if (idx >= 0) m_rxProfile->setCurrentIndex(idx);
    idx = m_txProfile->findData(tx); if (idx >= 0) m_txProfile->setCurrentIndex(idx);
    const QString quality = valueOr(settings, QStringLiteral("quality"), QStringLiteral("medium"));
    m_quality->setCurrentIndex(quality == QStringLiteral("low") ? 0 : quality == QStringLiteral("high") ? 2 : 1);
    connect(m_rxProfile, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) { m_settings.insert(QStringLiteral("rxProfile"),m_rxProfile->currentData().toString()); saveSettings(m_settings); });
    connect(m_txProfile, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) { m_settings.insert(QStringLiteral("txProfile"),m_txProfile->currentData().toString()); saveSettings(m_settings); });
    connect(m_quality, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int index) { m_settings.insert(QStringLiteral("quality"),index==0?QStringLiteral("low"):index==2?QStringLiteral("high"):QStringLiteral("medium")); saveSettings(m_settings); });
    connect(m_template, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) { QString error; const auto image=renderTemplate(&error); if(!image.isNull()){m_txPreview->setSourceImage(image);}else if(!error.isEmpty())appendLog(error); });
    refreshTemplates(); refreshGallery(); refreshAlbum();
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
    int index=m_template->findData(current); if(index>=0)m_template->setCurrentIndex(index);
}

void MainWindow::refreshGallery() {
    QString error; const auto result=m_backend->callSync(QStringLiteral("GetGallery"),{},&error);
    if(error.isEmpty())m_gallery=result.toArray(); else appendLog(error);
}

void MainWindow::refreshAlbum() {
    QString error; const auto result=m_backend->callSync(QStringLiteral("GetAlbum"),{},&error);
    if(!error.isEmpty()){appendLog(error);return;} m_album=result.toArray(); m_albumTable->setRowCount(m_album.size());
    for(int row=0;row<m_album.size();++row){const auto item=m_album.at(row).toObject();
        const QStringList values={item.value(QStringLiteral("type")).toString(),item.value(QStringLiteral("createdAt")).toString(),item.value(QStringLiteral("callsign")).toString(),item.value(QStringLiteral("profile")).toString(),QString::number(item.value(QStringLiteral("imageId")).toInt()),QStringLiteral("%1/%2").arg(item.value(QStringLiteral("packetsReceived")).toInt()).arg(item.value(QStringLiteral("packetsExpected")).toInt()),item.value(QStringLiteral("status")).toString()};
        for(int col=0;col<values.size();++col){auto *cell=new QTableWidgetItem(values[col]);cell->setData(Qt::UserRole,item.value(QStringLiteral("id")).toString());m_albumTable->setItem(row,col,cell);}
    }
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
        m_rxStatus->setText(QStringLiteral("%1 · %2 · 图像 ID %3 · %4/%5 包 · 覆盖率 %6% · %7")
            .arg(payload.value(QStringLiteral("profile")).toString(),payload.value(QStringLiteral("callsign")).toString())
            .arg(payload.value(QStringLiteral("imageId")).toInt()).arg(payload.value(QStringLiteral("packetsReceived")).toInt()).arg(payload.value(QStringLiteral("packetsExpected")).toInt())
            .arg(payload.value(QStringLiteral("coverage")).toDouble()*100.0,0,'f',1).arg(payload.value(QStringLiteral("complete")).toBool()?QStringLiteral("完成"):QStringLiteral("接收中")));
        if(payload.value(QStringLiteral("complete")).toBool())refreshAlbum();
    } else if(name==QStringLiteral("rx:level")){
        const auto p=payloadValue.toObject();m_rxStatus->setToolTip(QStringLiteral("RMS %1 dBFS · Peak %2").arg(p.value(QStringLiteral("rmsDbfs")).toDouble(),0,'f',1).arg(p.value(QStringLiteral("peak")).toDouble(),0,'f',3));
    } else if(name==QStringLiteral("rx:spectrum")||name==QStringLiteral("tx:spectrum")){
        const auto bins=payloadValue.toArray(); if(!bins.isEmpty()){
            m_spectrum->pushBins(bins);
            double peak=-120.0;int peakHz=0;for(const auto &b:bins){const auto o=b.toObject();const double db=o.value(QStringLiteral("magnitudeDbfs")).toDouble(-120);if(db>peak){peak=db;peakHz=o.value(QStringLiteral("frequencyHz")).toInt();}}
            m_spectrumStatus->setText(QStringLiteral("%1 · %2 bins · 峰值 %3 dBFS @ %4 Hz").arg(name.startsWith(QStringLiteral("rx"))?QStringLiteral("RX"):QStringLiteral("TX")).arg(bins.size()).arg(peak,0,'f',1).arg(peakHz));
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
void MainWindow::showError(const QString &message){appendLog(QStringLiteral("错误：")+message);statusBar()->showMessage(message,10000);QMessageBox::warning(this,QStringLiteral("SATSSDV-Go"),message);}
