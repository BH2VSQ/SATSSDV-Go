#include "mainwindow.h"
#include "template_canvas.h"
#include "alpha_outline.h"
#include "ui_i18n.h"

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
#include <QColorDialog>
#include <QStyle>
#include <QTabBar>
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
#include <QMovie>
#include <QImageReader>
#include <QDebug>
#include <QCryptographicHash>
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
#include <utility>
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
        setText(uiText(QStringLiteral("等待图像")));
        setStyleSheet(QStringLiteral("QLabel { background:#101b25; color:#91a7b6; border:1px solid #344553; border-radius:8px; }"));
        setScaledContents(false);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    void setSourceImage(const QImage &image) {
        clearMovie();
        m_source = image;
        updatePixmap();
    }

    bool setSourceGIF(const QByteArray &data, QString *errorMessage = nullptr) {
        const QByteArray digest = QCryptographicHash::hash(data, QCryptographicHash::Sha256);
        // RX events may request the exact same completed GIF more than once.
        // Keep the active QMovie instead of resetting it to frame zero.
        if (m_movie && digest == m_movieDigest)
            return true;

        clearMovie();
        m_source = QImage();
        setPixmap(QPixmap());
        setText(QString());
        if (data.size() < 13 || !(data.startsWith("GIF87a") || data.startsWith("GIF89a"))) {
            if (errorMessage) *errorMessage = QStringLiteral("invalid GIF header or truncated data");
            return false;
        }

        m_movieBuffer = new QBuffer(this);
        m_movieBuffer->setData(data);
        if (!m_movieBuffer->open(QIODevice::ReadOnly)) {
            if (errorMessage) *errorMessage = QStringLiteral("unable to open GIF data buffer");
            clearMovie();
            return false;
        }

        m_movie = new QMovie(m_movieBuffer, QByteArrayLiteral("gif"), this);
        // Keep only the current decoded frame. CacheAll retains every full-size frame
        // and makes long RX animations consume memory proportional to frame count.
        m_movie->setCacheMode(QMovie::CacheNone);
        if (!m_movie->isValid() || !m_movie->jumpToFrame(0)) {
            if (errorMessage) {
                const QString detail = m_movie->lastErrorString();
                *errorMessage = detail.isEmpty()
                    ? QStringLiteral("Qt GIF decoder rejected the image") : detail;
            }
            clearMovie();
            return false;
        }

        m_movieOriginalSize = m_movie->currentImage().size();
        updateMovieScale();
        if (!m_movie->jumpToFrame(0)) {
            if (errorMessage) *errorMessage = QStringLiteral("unable to decode the first GIF frame");
            clearMovie();
            return false;
        }

        // QMovie owns the playback clock and frame timing. Do not rely on
        // QLabel::setMovie(): explicitly repaint the current frame whenever
        // QMovie reports a decoded-frame update, which also works with custom
        // preview labels and avoids the previous frozen-first-frame behavior.
        connect(m_movie, &QMovie::updated, this, [this](const QRect &) {
            displayMovieFrame();
        });
        connect(m_movie, &QMovie::resized, this, [this](const QSize &) {
            displayMovieFrame();
        });
        connect(m_movie, &QMovie::error, this, [this](QImageReader::ImageReaderError) {
            const QString detail = m_movie ? m_movie->lastErrorString() : QString();
            qWarning().noquote() << "RX GIF playback error:" << detail;
        });

        m_movieDigest = digest;
        displayMovieFrame();
        m_movie->start();
        if (m_movie->state() != QMovie::Running) {
            if (errorMessage) {
                const QString detail = m_movie->lastErrorString();
                *errorMessage = detail.isEmpty()
                    ? QStringLiteral("Qt GIF playback did not enter the running state") : detail;
            }
            clearMovie();
            return false;
        }
        return true;
    }

    void clearSourceImage() {
        clearMovie();
        m_source = QImage();
        setPixmap(QPixmap());
        setText(uiText(QStringLiteral("等待图像")));
    }

protected:
    void resizeEvent(QResizeEvent *event) override {
        QLabel::resizeEvent(event);
        if (m_movie) updateMovieScale();
        else updatePixmap();
    }

private:
    void clearMovie() {
        if (m_movie) {
            m_movie->stop();
            setMovie(nullptr);
            delete m_movie;
            m_movie = nullptr;
        }
        if (m_movieBuffer) {
            m_movieBuffer->close();
            delete m_movieBuffer;
            m_movieBuffer = nullptr;
        }
        m_movieOriginalSize = QSize();
        m_movieDigest.clear();
    }

    void updateMovieScale() {
        if (!m_movie || m_movieOriginalSize.isEmpty() || contentsRect().isEmpty()) return;
        const QSize scaled = m_movieOriginalSize.scaled(contentsRect().size(), Qt::KeepAspectRatio);
        if (!scaled.isEmpty()) {
            m_movie->setScaledSize(scaled);
            displayMovieFrame();
        }
    }

    void displayMovieFrame() {
        if (!m_movie) return;
        const QPixmap frame = m_movie->currentPixmap();
        if (frame.isNull()) return;
        setText(QString());
        setPixmap(frame.scaled(contentsRect().size(), Qt::KeepAspectRatio, Qt::SmoothTransformation));
    }

    void updatePixmap() {
        if (m_source.isNull()) {
            setPixmap(QPixmap());
            setText(uiText(QStringLiteral("等待图像")));
            return;
        }
        if (contentsRect().isEmpty()) return;
        setText(QString());
        setPixmap(QPixmap::fromImage(m_source.scaled(contentsRect().size(), Qt::KeepAspectRatio, Qt::SmoothTransformation)));
    }

    QImage m_source;
    QBuffer *m_movieBuffer = nullptr;
    QMovie *m_movie = nullptr;
    QSize m_movieOriginalSize;
    QByteArray m_movieDigest;
};
class SpectrumWaterfallWidget final : public QWidget {
public:
    explicit SpectrumWaterfallWidget(QWidget *parent = nullptr)
        : QWidget(parent), m_waterfall(1280, 220, QImage::Format_RGB32) {
        m_waterfall.fill(m_backgroundColor);
        m_decayTimer.setTimerType(Qt::PreciseTimer);
        m_decayTimer.setInterval(1000 / m_rowsPerSecond);
        connect(&m_decayTimer, &QTimer::timeout, this, [this] { appendBlankRow(); });
        setMinimumHeight(95);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    // TX audio stops between repeat bursts. Continue inserting empty rows at
    // the configured waterfall rate so the final burst scrolls upward and
    // naturally disappears instead of remaining frozen on screen.
    void setStreaming(bool active) {
        if (active) {
            m_streaming = true;
            m_decayTimer.stop();
            m_decayRowsRemaining = 0;
            return;
        }
        m_streaming = false;
        if (m_hasData && !m_decayTimer.isActive()) {
            m_decayRowsRemaining = m_waterfall.height();
            m_decayTimer.setInterval(qMax(25, 1000 / qMax(1, m_rowsPerSecond)));
            m_decayTimer.start();
        }
    }

    void setDisplaySettings(const QJsonObject &settings) {
        m_minFrequencyHz = qBound(0, settings.value(QStringLiteral("minFrequencyHz")).toInt(0), 47000);
        m_maxFrequencyHz = qBound(100, settings.value(QStringLiteral("maxFrequencyHz")).toInt(5000), 48000);
        if (m_maxFrequencyHz <= m_minFrequencyHz) m_maxFrequencyHz = qMin(48000, m_minFrequencyHz + 1000);
        m_minDbfs = qBound(-120, settings.value(QStringLiteral("minDbfs")).toInt(-110), -10);
        m_maxDbfs = qBound(-90, settings.value(QStringLiteral("maxDbfs")).toInt(-20), 0);
        if (m_maxDbfs <= m_minDbfs + 4) m_maxDbfs = qMin(0, m_minDbfs + 5);
        m_rowsPerSecond = qBound(1, settings.value(QStringLiteral("rowsPerSecond")).toInt(15), 40);
        m_historySeconds = qBound(5, settings.value(QStringLiteral("historySeconds")).toInt(20), 60);
        const auto color = [&settings](const QString &key, const QColor &fallback) {
            const QColor parsed(settings.value(key).toString());
            return parsed.isValid() ? parsed : fallback;
        };
        m_backgroundColor = color(QStringLiteral("backgroundColor"), QColor(QStringLiteral("#07111b")));
        m_lowColor = color(QStringLiteral("lowColor"), QColor(QStringLiteral("#153a78")));
        m_midColor = color(QStringLiteral("midColor"), QColor(QStringLiteral("#168fae")));
        m_highColor = color(QStringLiteral("highColor"), QColor(QStringLiteral("#39c6b0")));
        m_peakColor = color(QStringLiteral("peakColor"), QColor(QStringLiteral("#f7d45b")));
        const int historyRows = qBound(30, m_rowsPerSecond * m_historySeconds, 2400);
        m_waterfall = QImage(1280, historyRows, QImage::Format_RGB32);
        m_waterfall.fill(m_backgroundColor);
        m_lastUpdateMs = 0;
        m_decayTimer.setInterval(qMax(25, 1000 / qMax(1, m_rowsPerSecond)));
        m_decayTimer.stop();
        m_decayRowsRemaining = 0;
        m_hasData = false;
        update();
    }

    void pushBins(const QJsonArray &bins) {
        if (bins.size() < 2) return;
        const qint64 now = QDateTime::currentMSecsSinceEpoch();
        const qint64 interval = qMax<qint64>(1, 1000 / qMax(1, m_rowsPerSecond));
        if (m_lastUpdateMs > 0 && now - m_lastUpdateMs < interval) return;
        m_lastUpdateMs = now;
        m_hasData = true;
        m_streaming = true;
        m_decayTimer.stop();
        m_decayRowsRemaining = 0;
        QVector<double> values;
        values.reserve(bins.size());
        double firstHz = bins.first().toObject().value(QStringLiteral("frequencyHz")).toDouble(0.0);
        double lastHz = firstHz;
        for (const auto &value : bins) {
            const QJsonObject bin = value.toObject();
            values.push_back(bin.value(QStringLiteral("magnitudeDbfs")).toDouble(-120.0));
            lastHz = bin.value(QStringLiteral("frequencyHz")).toDouble(lastHz);
        }
        if (values.size() < 2) return;
        m_dataMinFrequencyHz = firstHz;
        m_dataMaxFrequencyHz = lastHz;
        if (m_waterfall.width() != 1280 || m_waterfall.height() != qBound(30, m_rowsPerSecond * m_historySeconds, 2400)) {
            m_waterfall = QImage(1280, qBound(30, m_rowsPerSecond * m_historySeconds, 2400), QImage::Format_RGB32);
            m_waterfall.fill(m_backgroundColor);
        }
        for (int y = 0; y < m_waterfall.height() - 1; ++y) {
            std::memcpy(m_waterfall.scanLine(y), m_waterfall.constScanLine(y + 1), size_t(m_waterfall.width()) * 4);
        }
        double minHz = qMax(m_dataMinFrequencyHz, double(m_minFrequencyHz));
        double maxHz = qMin(m_dataMaxFrequencyHz, double(m_maxFrequencyHz));
        if (maxHz <= minHz) { minHz = m_dataMinFrequencyHz; maxHz = m_dataMaxFrequencyHz; }
        m_visibleMinFrequencyHz = minHz;
        m_visibleMaxFrequencyHz = maxHz;
        QRgb *row = reinterpret_cast<QRgb *>(m_waterfall.scanLine(m_waterfall.height() - 1));
        const double sourceSpan = qMax(1.0, m_dataMaxFrequencyHz - m_dataMinFrequencyHz);
        for (int x = 0; x < m_waterfall.width(); ++x) {
            const double hz = minHz + (maxHz - minHz) * (double(x) / qMax(1, m_waterfall.width() - 1));
            const double sourcePosition = qBound(0.0, (hz - m_dataMinFrequencyHz) / sourceSpan * double(values.size() - 1), double(values.size() - 1));
            const int index0 = qBound(0, int(std::floor(sourcePosition)), int(values.size()) - 1);
            const int index1 = qMin(index0 + 1, int(values.size()) - 1);
            const double fraction = sourcePosition - index0;
            const double magnitude = values[index0] + (values[index1] - values[index0]) * fraction;
            const double level = qBound(0.0, (magnitude - m_minDbfs) / qMax(1.0, double(m_maxDbfs - m_minDbfs)), 1.0);
            row[x] = colorFor(level).rgb();
        }
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override {
        QPainter painter(this);
        painter.setRenderHint(QPainter::SmoothPixmapTransform, true);
        painter.fillRect(rect(), m_backgroundColor);

        const double minHz = m_visibleMaxFrequencyHz > m_visibleMinFrequencyHz
            ? m_visibleMinFrequencyHz : double(m_minFrequencyHz);
        const double maxHz = m_visibleMaxFrequencyHz > m_visibleMinFrequencyHz
            ? m_visibleMaxFrequencyHz : double(m_maxFrequencyHz);
        const qreal axisHeight = qMin<qreal>(22.0, qMax<qreal>(18.0, height() * 0.22));
        const QRectF waterfallRect(0, axisHeight, width(), qMax<qreal>(1.0, height() - axisHeight));
        painter.drawImage(waterfallRect, m_waterfall);

        // The waterfall is the only plot. Frequency labels are anchored to
        // the same visible range used to map incoming FFT bins into the image.
        painter.setFont(QFont(font().family(), 7));
        painter.setPen(QColor(QStringLiteral("#9fb3c0")));
        if (maxHz > minHz && width() > 0) {
            const int firstTickHz = qMax(1, int(std::ceil(minHz / 500.0 - 1e-9))) * 500;
            const int lastTickHz = int(std::floor(maxHz / 500.0 + 1e-9)) * 500;
            for (int frequencyHz = firstTickHz; frequencyHz <= lastTickHz; frequencyHz += 500) {
                const qreal x = (frequencyHz - minHz) / (maxHz - minHz) * qMax(0, width() - 1);
                QRectF labelRect(x - 28.0, 1.0, 56.0, axisHeight - 3.0);
                if (labelRect.left() < 0) labelRect.moveLeft(0);
                if (labelRect.right() > width()) labelRect.moveRight(width());
                painter.drawText(labelRect, Qt::AlignHCenter | Qt::AlignVCenter, QString::number(frequencyHz));
                painter.setPen(QPen(QColor(QStringLiteral("#365365")), 1));
                painter.drawLine(QPointF(x, axisHeight - 2.0), QPointF(x, height()));
                painter.setPen(QColor(QStringLiteral("#9fb3c0")));
            }
        }

        painter.setPen(QPen(QColor(QStringLiteral("#294252")), 1));
        for (int i = 1; i < 4; ++i) {
            const qreal y = waterfallRect.top() + waterfallRect.height() * i / 4.0;
            painter.drawLine(QPointF(0, y), QPointF(width(), y));
        }
    }

private:
    void appendBlankRow() {
        if (m_streaming || !m_hasData || m_waterfall.isNull()) {
            m_decayTimer.stop();
            return;
        }
        const qsizetype stride = m_waterfall.bytesPerLine();
        const qsizetype moveBytes = stride * qMax(0, m_waterfall.height() - 1);
        if (moveBytes > 0) {
            std::memmove(m_waterfall.bits(), m_waterfall.constBits() + stride, size_t(moveBytes));
        }
        QRgb *newest = reinterpret_cast<QRgb *>(m_waterfall.scanLine(m_waterfall.height() - 1));
        std::fill(newest, newest + m_waterfall.width(), m_backgroundColor.rgb());
        if (m_decayRowsRemaining > 0) --m_decayRowsRemaining;
        if (m_decayRowsRemaining <= 0) {
            m_decayTimer.stop();
            m_waterfall.fill(m_backgroundColor);
            m_hasData = false;
        }
        update();
    }

    QColor colorFor(double value) const {
        const QList<QColor> stops{m_backgroundColor, m_lowColor, m_midColor, m_highColor, m_peakColor};
        const double scaled = qBound(0.0, value, 1.0) * (stops.size() - 1);
        const int index = qMin(int(stops.size()) - 2, int(std::floor(scaled)));
        const double t = scaled - index;
        const QColor a = stops[index], b = stops[index + 1];
        return QColor::fromRgbF(a.redF() + (b.redF() - a.redF()) * t,
                                a.greenF() + (b.greenF() - a.greenF()) * t,
                                a.blueF() + (b.blueF() - a.blueF()) * t);
    }

    QImage m_waterfall;
    QTimer m_decayTimer;
    int m_decayRowsRemaining = 0;
    bool m_hasData = false;
    bool m_streaming = false;
    int m_minFrequencyHz = 0;
    int m_maxFrequencyHz = 5000;
    int m_minDbfs = -110;
    int m_maxDbfs = -20;
    int m_rowsPerSecond = 15;
    int m_historySeconds = 20;
    QColor m_backgroundColor{QStringLiteral("#07111b")};
    QColor m_lowColor{QStringLiteral("#153a78")};
    QColor m_midColor{QStringLiteral("#168fae")};
    QColor m_highColor{QStringLiteral("#39c6b0")};
    QColor m_peakColor{QStringLiteral("#f7d45b")};
    double m_dataMinFrequencyHz = 0.0;
    double m_dataMaxFrequencyHz = 24000.0;
    double m_visibleMinFrequencyHz = 0.0;
    double m_visibleMaxFrequencyHz = 0.0;
    qint64 m_lastUpdateMs = 0;
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

QString formatTXDuration(qint64 seconds) {
    seconds = qMax<qint64>(0, seconds);
    const qint64 hours = seconds / 3600;
    const qint64 minutes = (seconds / 60) % 60;
    const qint64 remainder = seconds % 60;
    return QStringLiteral("%1:%2:%3")
        .arg(hours, 2, 10, QLatin1Char('0'))
        .arg(minutes, 2, 10, QLatin1Char('0'))
        .arg(remainder, 2, 10, QLatin1Char('0'));
}

QString valueOr(const QJsonObject &object, const QString &key, const QString &fallback = QString()) {
    const QString value = object.value(key).toString();
    return value.isEmpty() ? fallback : value;
}

bool isGifGalleryAsset(const QJsonArray &gallery, const QString &assetId) {
    if (assetId.isEmpty()) return false;
    for (const QJsonValue &value : gallery) {
        const QJsonObject asset = value.toObject();
        if (asset.value(QStringLiteral("id")).toString() != assetId) continue;
        return asset.value(QStringLiteral("name")).toString().endsWith(QStringLiteral(".gif"), Qt::CaseInsensitive);
    }
    return false;
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
        m_audioStatus->setText(uiText(QStringLiteral("后端启动失败")));
        m_audioStatus->setToolTip(startupError);
        m_audioStatus->setStyleSheet(QStringLiteral("background:#f8eeee; border:1px solid #d7b2b7; padding:6px 10px; color:#98545c;"));
        if (m_rxToggle) m_rxToggle->setEnabled(false);
        if (m_txToggle) m_txToggle->setEnabled(false);
        if (m_txToneToggle) m_txToneToggle->setEnabled(false);
        statusBar()->showMessage(startupError.isEmpty() ? uiText(QStringLiteral("Go 后端不可用")) : uiText(QStringLiteral("Go 后端启动失败：%1")).arg(startupError));
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
        "QLabel { background:transparent; border:0; }"
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
        "QSlider { border:0; background:transparent; padding:0; }"
        "QSlider::groove:horizontal { height:5px; background:#d3dbe0; border:0; border-radius:2px; }"
        "QSlider::handle:horizontal { width:12px; background:#6fa5ba; border:0; border-radius:6px; margin:-4px 0; }"
        "QSlider::groove:vertical { width:5px; background:#d3dbe0; border:0; border-radius:2px; }"
        "QSlider::handle:vertical { height:12px; background:#6fa5ba; border:0; border-radius:6px; margin:0 -4px; }"
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
    header->setObjectName(QStringLiteral("appHeader"));
    header->setFixedHeight(58);
    header->setStyleSheet(QStringLiteral("QWidget#appHeader { background:linear-gradient(#ffffff,#e7e9eb); border-bottom:1px solid #aeb6bd; }"));
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
    headerLayout->addSpacing(16);
    m_workbenchNav = new QPushButton(uiText(QStringLiteral("工作台")), header);
    m_workbenchNav->setCheckable(true);
    m_workbenchNav->setChecked(true);
    m_workbenchNav->setMinimumWidth(78);
    m_albumNav = new QPushButton(uiText(QStringLiteral("相册")), header);
    m_albumNav->setCheckable(true);
    m_albumNav->setMinimumWidth(78);
    headerLayout->addWidget(m_workbenchNav);
    headerLayout->addWidget(m_albumNav);
    headerLayout->addStretch();
    m_languageSelect = makeCombo(header, {uiText(QStringLiteral("简体中文")), QStringLiteral("English"), uiText(QStringLiteral("日本語"))});
    m_languageSelect->setMinimumWidth(105);
    headerLayout->addWidget(m_languageSelect);
    m_audioStatus = new QLabel(uiText(QStringLiteral("音频：正在连接")), header);
    m_audioStatus->setMinimumWidth(205);
    m_audioStatus->setAlignment(Qt::AlignCenter);
    m_audioStatus->setStyleSheet(QStringLiteral("background:#f3f5f6; border:1px solid #c4cbd0; padding:6px 10px; color:#6b747c;"));
    auto *settingsButton = new QPushButton(QStringLiteral("⚙"), header);
    settingsButton->setToolTip(uiText(QStringLiteral("设备设置")));
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

    m_mainTabs = new QTabWidget(central);
    auto *tabs = m_mainTabs;
    tabs->setDocumentMode(true);
    tabs->setMovable(false);
    tabs->tabBar()->hide();
    connect(m_workbenchNav, &QPushButton::clicked, this, [this] {
        m_mainTabs->setCurrentIndex(0);
        m_workbenchNav->setChecked(true);
        m_albumNav->setChecked(false);
    });
    connect(m_albumNav, &QPushButton::clicked, this, [this] {
        m_mainTabs->setCurrentIndex(1);
        m_workbenchNav->setChecked(false);
        m_albumNav->setChecked(true);
        refreshAlbum();
    });
    connect(m_languageSelect, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int index) {
        const QString locale = index == 1 ? QStringLiteral("en") : index == 2 ? QStringLiteral("ja-JP") : QStringLiteral("zh-CN");
        applyLanguage(locale);
    });

    auto *workbench = new QWidget(tabs);
    auto *workLayout = new QVBoxLayout(workbench);
    workLayout->setContentsMargins(6, 6, 6, 6);
    workLayout->setSpacing(5);

    auto *profilePanel = new QWidget(workbench);
    profilePanel->setObjectName(QStringLiteral("profilePanel"));
    profilePanel->setStyleSheet(QStringLiteral("QWidget#profilePanel { background:#f1f3f4; border-bottom:1px solid #abb3ba; }"));
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
        profileLayout->addWidget(row);
        *target = buttons;
    };
    addProfileRow(QStringLiteral("RX PROFILE"), uiText(QStringLiteral("接收 RX")), &m_rxProfileButtons);
    addProfileRow(QStringLiteral("TX PROFILE"), uiText(QStringLiteral("发送 TX")), &m_txProfileButtons);
    workLayout->addWidget(profilePanel);

    auto *split = new QSplitter(Qt::Horizontal, workbench);
    split->setChildrenCollapsible(false);
    split->setHandleWidth(5);

    // RX workstation: image reconstruction occupies the main area; its own
    // receive spectrum/waterfall remains at the bottom of the RX column.
    auto *rxPane = new QWidget(split);
    rxPane->setObjectName(QStringLiteral("rxPane"));
    rxPane->setStyleSheet(QStringLiteral("QWidget#rxPane { background:#f2f4f5; border:1px solid #929da5; }"));
    auto *rxLayout = new QVBoxLayout(rxPane);
    rxLayout->setContentsMargins(8, 7, 8, 7);
    rxLayout->setSpacing(5);
    auto *rxHeader = new QHBoxLayout();
    auto *rxTitle = new QLabel(uiText(QStringLiteral("RECEIVE\n接收 RX")), rxPane);
    rxTitle->setStyleSheet(QStringLiteral("font-size:10pt; font-weight:700; color:#314b5a;"));
    m_rxStatus = new QLabel(uiText(QStringLiteral("RX 待机 · 等待接收音频")), rxPane);
    m_rxStatus->setStyleSheet(QStringLiteral("font-size:8pt; color:#77838b;"));
    m_rxStatus->setWordWrap(true);
    m_rxToggle = new QPushButton(uiText(QStringLiteral("启动 RX")), rxPane);
    m_rxToggle->setProperty("role", "primary");
    m_rxToggle->setMinimumWidth(105);
    connect(m_rxToggle, &QPushButton::clicked, this, [this] { toggleRX(); });
    rxHeader->addWidget(rxTitle);
    rxHeader->addStretch();
    rxHeader->addWidget(m_rxStatus, 1);
    rxHeader->addWidget(m_rxToggle);
    rxLayout->addLayout(rxHeader);
    m_rxPreview = makePreview(rxPane);
    m_rxPreview->setMinimumSize(340, 260);
    m_rxPreview->setStyleSheet(QStringLiteral("QLabel { background:#ffffff; color:#87939b; border:1px solid #a1aab1; border-radius:0; }"));
    rxLayout->addWidget(m_rxPreview, 5);
    m_rxMeta = new QLabel(QStringLiteral("CALL  ---   SSID  --   IMAGE ID  --   PACKETS  0 / 0   MODE  ---"), rxPane);
    m_rxMeta->setStyleSheet(QStringLiteral("font-size:8pt; color:#7e8991; padding:2px 1px;"));
    rxLayout->addWidget(m_rxMeta);
    m_rxDiagnostics = new QLabel(uiText(QStringLiteral("RX 解码诊断 · 等待音频")), rxPane);
    m_rxDiagnostics->setStyleSheet(QStringLiteral("font-size:7pt; color:#6b7881; padding:1px 1px;"));
    m_rxDiagnostics->setTextInteractionFlags(Qt::TextSelectableByMouse);
    m_rxDiagnostics->setWordWrap(true);
    rxLayout->addWidget(m_rxDiagnostics);
    auto *rxSpectrumTitle = new QHBoxLayout();
    rxSpectrumTitle->addWidget(new QLabel(uiText(QStringLiteral("RX 瀑布图")), rxPane));
    rxSpectrumTitle->addStretch();
    m_rxSnrLabel = new QLabel(uiText(QStringLiteral("SNR -- dB")), rxPane);
    m_rxSnrLabel->setObjectName(QStringLiteral("rxSnrLabel"));
    m_rxSnrLabel->setStyleSheet(QStringLiteral("font-size:8pt; color:#315c70; font-weight:700; padding:1px 4px;"));
    rxSpectrumTitle->addWidget(m_rxSnrLabel);
    rxLayout->addLayout(rxSpectrumTitle);
    m_rxSpectrumStatus = new QLabel(uiText(QStringLiteral("等待 RX 音频")), rxPane);
    m_rxSpectrumStatus->setStyleSheet(QStringLiteral("font-size:8pt; color:#77858f;"));
    m_rxSpectrumStatus->hide();
    m_rxSpectrum = new SpectrumWaterfallWidget(rxPane);
    m_rxSpectrum->setMinimumHeight(95);
    m_rxSpectrum->setMaximumHeight(155);
    m_rxSpectrum->setStyleSheet(QStringLiteral("border:1px solid #7e8c96;"));
    rxLayout->addWidget(m_rxSpectrum, 1);
    auto *rxFooter = new QLabel(uiText(QStringLiteral("LEVEL  -∞ dBFS       PEAK  ---       横轴：频率 · 纵轴：时间")), rxPane);
    rxFooter->setStyleSheet(QStringLiteral("font-size:8pt; color:#859099; padding:2px;"));
    rxLayout->addWidget(rxFooter);

    // TX workstation: all common transmission controls remain in one compact
    // strip, with the image canvas/QSO fields and template tools side-by-side.
    auto *txPane = new QWidget(split);
    txPane->setObjectName(QStringLiteral("txPane"));
    txPane->setStyleSheet(QStringLiteral("QWidget#txPane { background:#f2f4f5; border:1px solid #929da5; }"));
    auto *txLayout = new QVBoxLayout(txPane);
    txLayout->setContentsMargins(8, 7, 8, 7);
    txLayout->setSpacing(5);
    auto *txHeader = new QHBoxLayout();
    auto *txTitle = new QLabel(uiText(QStringLiteral("TRANSMIT\n发送 TX")), txPane);
    txTitle->setStyleSheet(QStringLiteral("font-size:10pt; font-weight:700; color:#314b5a;"));
    txHeader->addWidget(txTitle);
    txHeader->addStretch();
    m_ax25SSIDField = new QWidget(txPane);
    auto *ax25SSIDLayout = new QHBoxLayout(m_ax25SSIDField);
    ax25SSIDLayout->setContentsMargins(0, 0, 0, 0);
    ax25SSIDLayout->setSpacing(4);
    ax25SSIDLayout->addWidget(new QLabel(uiText(QStringLiteral("AX.25 SSID")), m_ax25SSIDField));
    m_ax25SSID = new QSpinBox(m_ax25SSIDField);
    m_ax25SSID->setRange(0, 15);
    m_ax25SSID->setValue(0);
    m_ax25SSID->setFixedWidth(64);
    ax25SSIDLayout->addWidget(m_ax25SSID);
    txHeader->addWidget(m_ax25SSIDField);
    txHeader->addWidget(new QLabel(uiText(QStringLiteral("图片质量")), txPane));
    m_quality = makeCombo(txPane, {uiText(QStringLiteral("低质量 320×240")), uiText(QStringLiteral("中质量 448×336")), uiText(QStringLiteral("高质量 640×480"))});
    m_quality->setCurrentIndex(1);
    m_quality->setMinimumWidth(125);
    txHeader->addWidget(m_quality);
    m_repeat = new QCheckBox(uiText(QStringLiteral("循环发送")), txPane);
    txHeader->addWidget(m_repeat);
    m_repeatInterval = new QSpinBox(txPane);
    m_repeatInterval->setRange(0, 86400);
    m_repeatInterval->setValue(120);
    m_repeatInterval->setSuffix(uiText(QStringLiteral(" 秒")));
    m_repeatInterval->setMaximumWidth(90);
    txHeader->addWidget(m_repeatInterval);
    m_txToggle = new QPushButton(uiText(QStringLiteral("开始发射")), txPane);
    m_txToggle->setProperty("role", "primary");
    m_txToggle->setMinimumWidth(112);
    m_txToneToggle = new QPushButton(uiText(QStringLiteral("1500 Hz 测试音")), txPane);
    m_txToneToggle->setMinimumWidth(118);
    txHeader->addWidget(m_txToggle);
    txHeader->addWidget(m_txToneToggle);
    txLayout->addLayout(txHeader);
    connect(m_txToggle, &QPushButton::clicked, this, [this] { toggleTX(); });
    connect(m_txToneToggle, &QPushButton::clicked, this, [this] {
        if (m_txToneActive) {
            m_backend->call(QStringLiteral("StopTX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (!ok) showError(error);
            });
            return;
        }
        m_backend->call(QStringLiteral("Start1500HzTestTone"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
            if (ok) appendLog(uiText(QStringLiteral("已启动 1500 Hz 测试音"))); else showError(error);
        });
    });
    connect(m_repeat, &QCheckBox::toggled, this, [this](bool enabled) {
        if (!m_repeatInterval) return;
        if (enabled) {
            m_repeatIntervalSeconds = m_repeatInterval->value();
            updateRepeatControls();
            return;
        }

        // Repeat can be toggled off only while the scheduler is in its
        // inter-cycle countdown. The current image burst is never interrupted.
        const bool cancelRepeatCountdown = m_txActive && m_txCountdownActive;
        m_txCountdownActive = false;
        m_repeatInterval->setValue(m_repeatIntervalSeconds);
        updateRepeatControls();
        if (cancelRepeatCountdown && m_backend && m_backend->isRunning()) {
            m_backend->call(QStringLiteral("StopTX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (!ok) showError(error);
            });
        }
    });
    connect(m_repeatInterval, QOverload<int>::of(&QSpinBox::valueChanged), this, [this](int value) {
        if (!m_repeat || !m_repeat->isChecked()) m_repeatIntervalSeconds = value;
    });
    updateRepeatControls();

    auto *templateStrip = new QWidget(txPane);
    auto *templateStripLayout = new QGridLayout(templateStrip);
    templateStripLayout->setContentsMargins(0, 2, 0, 5);
    templateStripLayout->setHorizontalSpacing(6);
    templateStripLayout->setVerticalSpacing(4);
    templateStripLayout->addWidget(new QLabel(uiText(QStringLiteral("预设名称")), templateStrip), 0, 0);
    m_templateName = new QLineEdit(templateStrip);
    m_templateName->setMaxLength(48);
    templateStripLayout->addWidget(m_templateName, 0, 1);
    auto *savePreset = new QPushButton(uiText(QStringLiteral("保存预设")), templateStrip);
    templateStripLayout->addWidget(savePreset, 0, 2);

    templateStripLayout->addWidget(new QLabel(uiText(QStringLiteral("发射模板")), templateStrip), 1, 0);
    m_template = makeCombo(templateStrip, {});
    templateStripLayout->addWidget(m_template, 1, 1);
    auto *newPreset = new QPushButton(uiText(QStringLiteral("＋ 新建预设")), templateStrip);
    auto *duplicatePreset = new QPushButton(uiText(QStringLiteral("复制当前")), templateStrip);
    auto *deletePreset = new QPushButton(uiText(QStringLiteral("删除当前")), templateStrip);
    auto *background = new QPushButton(uiText(QStringLiteral("背景图")), templateStrip);
    auto *importPack = new QPushButton(uiText(QStringLiteral("导入模板包")), templateStrip);
    auto *exportPack = new QPushButton(uiText(QStringLiteral("导出模板包")), templateStrip);
    deletePreset->setProperty("role", "danger");
    templateStripLayout->addWidget(newPreset, 1, 2);
    templateStripLayout->addWidget(duplicatePreset, 1, 3);
    templateStripLayout->addWidget(deletePreset, 1, 4);
    templateStripLayout->addWidget(background, 1, 5);
    templateStripLayout->addWidget(importPack, 1, 6);
    templateStripLayout->addWidget(exportPack, 1, 7);
    templateStripLayout->setColumnStretch(1, 1);
    connect(background, &QPushButton::clicked, this, [this] { selectTemplateBackground(); });
    connect(importPack, &QPushButton::clicked, this, [this] { importTemplatePack(); });
    connect(exportPack, &QPushButton::clicked, this, [this] { exportTemplatePack(); });
    connect(m_template, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) {
        const QJsonObject selected = selectedTemplate();
        if (m_templateName) m_templateName->setText(selected.value(QStringLiteral("name")).toString());
        clearHisCallForCQTemplate();
        refreshTemplateComponentList();
        refreshTemplateCanvas();
        QString error;
        const QImage image = renderTemplate(&error);
        if (!image.isNull()) m_txPreview->setSourceImage(image);
        else if (!error.isEmpty()) appendLog(error);
    });
    connect(savePreset, &QPushButton::clicked, this, [this] {
        const QString id = m_template->currentData().toString();
        const QString name = m_templateName ? m_templateName->text().trimmed() : QString();
        if (id.isEmpty() || name.isEmpty()) {
            statusBar()->showMessage(uiText(QStringLiteral("预设名称不能为空")), 5000);
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
            refreshTemplateCanvas();
            statusBar()->showMessage(uiText(QStringLiteral("模板预设已保存")), 4000);
        });
    });
    connect(newPreset, &QPushButton::clicked, this, [this] {
        if (m_templates.size() >= 32) { statusBar()->showMessage(uiText(QStringLiteral("最多支持 32 个模板预设")), 5000); return; }
        QJsonObject item;
        const QString id = QUuid::createUuid().toString(QUuid::WithoutBraces);
        item.insert(QStringLiteral("id"), id);
        item.insert(QStringLiteral("name"), uiText(QStringLiteral("新建预设")));
        item.insert(QStringLiteral("components"), QJsonArray{});
        item.insert(QStringLiteral("updatedAt"), QDateTime::currentDateTimeUtc().toString(Qt::ISODate));
        QJsonArray updated = m_templates;
        updated.append(item);
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this, id](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            const int index = m_template->findData(id);
            if (index >= 0) m_template->setCurrentIndex(index);
            m_templateName->setText(uiText(QStringLiteral("新建预设")));
            statusBar()->showMessage(uiText(QStringLiteral("已创建空白模板预设")), 4000);
        });
    });
    connect(duplicatePreset, &QPushButton::clicked, this, [this] {
        if (m_templates.size() >= 32) { statusBar()->showMessage(uiText(QStringLiteral("最多支持 32 个模板预设")), 5000); return; }
        QJsonObject item = selectedTemplate();
        if (item.isEmpty()) return;
        const QString id = QUuid::createUuid().toString(QUuid::WithoutBraces);
        item.insert(QStringLiteral("id"), id);
        item.insert(QStringLiteral("name"), item.value(QStringLiteral("name")).toString().left(44) + uiText(QStringLiteral(" 副本")));
        item.insert(QStringLiteral("updatedAt"), QDateTime::currentDateTimeUtc().toString(Qt::ISODate));
        QJsonArray updated = m_templates;
        updated.append(item);
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this, id](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            const int index = m_template->findData(id);
            if (index >= 0) m_template->setCurrentIndex(index);
            m_templateName->setText(m_template->currentText());
            statusBar()->showMessage(uiText(QStringLiteral("当前模板已复制")), 4000);
        });
    });
    connect(deletePreset, &QPushButton::clicked, this, [this] {
        const QString id = m_template->currentData().toString();
        if (id.isEmpty()) return;
        if (m_templates.size() <= 1) { statusBar()->showMessage(uiText(QStringLiteral("至少需要保留一个模板预设")), 5000); return; }
        if (QMessageBox::question(this, uiText(QStringLiteral("删除模板预设")), uiText(QStringLiteral("确定删除当前模板预设吗？"))) != QMessageBox::Yes) return;
        QJsonArray updated;
        for (const QJsonValue &value : m_templates) {
            if (value.toObject().value(QStringLiteral("id")).toString() != id) updated.append(value);
        }
        m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{updated}, [this](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) { showError(error); return; }
            refreshTemplates();
            statusBar()->showMessage(uiText(QStringLiteral("模板预设已删除")), 4000);
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
    m_txPreview->hide(); // retained as the rasterized transmit image source
    m_templateCanvas = new TemplateCanvas(txPreviewColumn);
    m_templateCanvas->setMinimumSize(330, 230);
    m_templateCanvas->setStyleSheet(QStringLiteral("background:#101923; border:1px solid #9da6ad;"));
    txPreviewLayout->addWidget(m_templateCanvas, 4);
    m_templateCanvas->setComponentsChangedCallback([this](const QJsonArray &updated) {
        const QString selectedId = m_template ? m_template->currentData().toString() : QString();
        for (int i = 0; i < m_templates.size(); ++i) {
            QJsonObject item = m_templates.at(i).toObject();
            if (item.value(QStringLiteral("id")).toString() == selectedId) {
                item.insert(QStringLiteral("components"), updated);
                m_templates.replace(i, item);
                break;
            }
        }
        bool listOutOfSync = !m_templateComponentList || m_templateComponentList->count() != updated.size();
        if (!listOutOfSync && m_templateComponentList) {
            for (int i = 0; i < updated.size(); ++i) {
                if (m_templateComponentList->item(i)->data(Qt::UserRole).toString() != updated.at(i).toObject().value(QStringLiteral("id")).toString()) {
                    listOutOfSync = true;
                    break;
                }
            }
        }
        if (listOutOfSync) refreshTemplateComponentList();
        if (m_templateCanvas && m_templateComponentList && m_templateCanvas->selectedIndex() != m_templateComponentList->currentRow())
            m_templateCanvas->setSelectedIndex(m_templateComponentList->currentRow());
        const int selectedIndex = m_templateCanvas ? m_templateCanvas->selectedIndex() : -1;
        const QJsonArray currentComponents = selectedTemplate().value(QStringLiteral("components")).toArray();
        if (selectedIndex >= 0 && selectedIndex < currentComponents.size()) {
            const QJsonObject selectedComponent = currentComponents.at(selectedIndex).toObject();
            const QSignalBlocker scaleBlocker(m_componentScaleSlider), fontBlocker(m_componentFontSlider);
            if (m_componentScaleSlider) m_componentScaleSlider->setValue(qBound(5, selectedComponent.value(QStringLiteral("scale")).toInt(35), 100));
            if (m_componentFontSlider) m_componentFontSlider->setValue(qBound(6, selectedComponent.value(QStringLiteral("fontSize")).toInt(28), 160));
        }
        if (m_templateComponentsSummary) m_templateComponentsSummary->setText(uiText(QStringLiteral("修改已更新到当前预设，点击‘保存预设’后写入配置。")));
    });
    m_templateCanvas->setSelectionChangedCallback([this](int index) {
        if (!m_templateComponentList) return;
        const QSignalBlocker blocker(m_templateComponentList);
        m_templateComponentList->setCurrentRow(index);
        refreshInlineTemplateEditor();
    });
    auto *qsoBox = new QGroupBox(uiText(QStringLiteral("本次 QSO 信息")), txPreviewColumn);
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
    addQsoField(0, 0, uiText(QStringLiteral("本台呼号")), &m_callsign);
    addQsoField(0, 1, uiText(QStringLiteral("对方呼号")), &m_hisCall);
    m_hisCall->setPlaceholderText(uiText(QStringLiteral("N0TEST")));
    connect(m_hisCall, &QLineEdit::textEdited, this, [this] { m_hisCallManuallyEdited = true; });
    addQsoField(0, 2, QStringLiteral("QTH"), &m_qth);
    addQsoField(1, 0, uiText(QStringLiteral("网格定位")), &m_locator);
    addQsoField(1, 1, uiText(QStringLiteral("RST / 报告")), &m_report, QStringLiteral("595"));
    addQsoField(1, 2, uiText(QStringLiteral("消息")), &m_message);
    qsoGrid->setColumnStretch(0, 1);
    qsoGrid->setColumnStretch(1, 1);
    qsoGrid->setColumnStretch(2, 1);
    txPreviewLayout->addWidget(qsoBox, 1);
    const QList<QLineEdit *> liveTemplateFields = {m_callsign, m_hisCall, m_qth, m_locator, m_report, m_message};
    for (QLineEdit *field : liveTemplateFields) {
        connect(field, &QLineEdit::textChanged, this, [this] { refreshTemplateCanvas(); });
    }

    auto *templateEditor = new QGroupBox(uiText(QStringLiteral("模板编辑")), txMain);
    auto *editorLayout = new QVBoxLayout(templateEditor);
    editorLayout->setContentsMargins(7, 8, 7, 7);
    editorLayout->setSpacing(5);
    m_templateComponentsSummary = new QLabel(uiText(QStringLiteral("模板编辑")), templateEditor);
    m_templateComponentsSummary->setWordWrap(true);
    m_templateComponentsSummary->setStyleSheet(QStringLiteral("color:#6d7982; font-size:8pt;"));
    editorLayout->addWidget(m_templateComponentsSummary);
    auto *componentActions = new QHBoxLayout();
    auto *addText = new QPushButton(uiText(QStringLiteral("＋ 文字")), templateEditor);
    auto *addImage = new QPushButton(uiText(QStringLiteral("＋ 图库图像")), templateEditor);
    auto *addLastRX = new QPushButton(uiText(QStringLiteral("＋ 上次接收图像")), templateEditor);
    auto *clearBg = new QPushButton(uiText(QStringLiteral("清除底图")), templateEditor);
    componentActions->addWidget(addText);
    componentActions->addWidget(addImage);
    componentActions->addWidget(addLastRX);
    componentActions->addWidget(clearBg);
    editorLayout->addLayout(componentActions);
    m_templateComponentList = new QListWidget(templateEditor);
    m_templateComponentList->setViewMode(QListView::IconMode);
    m_templateComponentList->setFlow(QListView::LeftToRight);
    m_templateComponentList->setWrapping(true);
    m_templateComponentList->setResizeMode(QListView::Adjust);
    m_templateComponentList->setMovement(QListView::Static);
    m_templateComponentList->setGridSize(QSize(132, 36));
    m_templateComponentList->setMinimumHeight(65);
    m_templateComponentList->setMaximumHeight(105);
    m_templateComponentList->setStyleSheet(QStringLiteral("QListWidget { background:#f8f9fa; border:1px solid #c4cbd0; } QListWidget::item { padding:4px; border-bottom:1px solid #e1e5e8; } QListWidget::item:selected { background:#dceef5; color:#315c70; }"));
    editorLayout->addWidget(m_templateComponentList);

    auto *propertyForm = new QFormLayout();
    propertyForm->setContentsMargins(0, 3, 0, 0);
    propertyForm->setHorizontalSpacing(7);
    propertyForm->setVerticalSpacing(4);
    m_componentTextEdit = new QTextEdit(templateEditor);
    m_componentTextEdit->setPlaceholderText(uiText(QStringLiteral("文字内容 / {MYCALL} / {LOCATOR}")));
    m_componentTextEdit->setMaximumHeight(54);
    m_componentTextLabel = new QLabel(uiText(QStringLiteral("文字内容")), templateEditor);
    propertyForm->addRow(m_componentTextLabel, m_componentTextEdit);
    m_componentAssetCombo = makeCombo(templateEditor, {});
    m_componentAssetLabel = new QLabel(uiText(QStringLiteral("图库图像")), templateEditor);
    propertyForm->addRow(m_componentAssetLabel, m_componentAssetCombo);
    m_componentScaleSlider = new QSlider(Qt::Horizontal, templateEditor);
    m_componentScaleSlider->setRange(5, 100);
    m_componentScaleLabel = new QLabel(uiText(QStringLiteral("图像缩放")), templateEditor);
    propertyForm->addRow(m_componentScaleLabel, m_componentScaleSlider);
    m_componentFontSlider = new QSlider(Qt::Horizontal, templateEditor);
    m_componentFontSlider->setRange(6, 160);
    m_componentFontLabel = new QLabel(uiText(QStringLiteral("文字字号")), templateEditor);
    propertyForm->addRow(m_componentFontLabel, m_componentFontSlider);
    m_componentOutlineSlider = new QSlider(Qt::Horizontal, templateEditor);
    m_componentOutlineSlider->setRange(0, 20);
    m_componentOutlineLabel = new QLabel(uiText(QStringLiteral("描边宽度")), templateEditor);
    propertyForm->addRow(m_componentOutlineLabel, m_componentOutlineSlider);
    editorLayout->addLayout(propertyForm);
    auto *colorRow = new QHBoxLayout();
    m_componentColorButton = new QPushButton(uiText(QStringLiteral("文字颜色")), templateEditor);
    m_componentOutlineColorButton = new QPushButton(uiText(QStringLiteral("描边颜色")), templateEditor);
    colorRow->addWidget(m_componentColorButton);
    colorRow->addWidget(m_componentOutlineColorButton);
    editorLayout->addLayout(colorRow);
    auto *flagsRow = new QHBoxLayout();
    m_componentBoldCheck = new QCheckBox(uiText(QStringLiteral("粗体")), templateEditor);
    m_componentVisibleCheck = new QCheckBox(uiText(QStringLiteral("显示")), templateEditor);
    m_componentOutlineCheck = new QCheckBox(uiText(QStringLiteral("描边")), templateEditor);
    flagsRow->addWidget(m_componentBoldCheck);
    flagsRow->addWidget(m_componentVisibleCheck);
    flagsRow->addWidget(m_componentOutlineCheck);
    flagsRow->addStretch();
    auto *deleteComponent = new QPushButton(uiText(QStringLiteral("删除")), templateEditor);
    deleteComponent->setProperty("role", "danger");
    flagsRow->addWidget(deleteComponent);
    editorLayout->addLayout(flagsRow);
    connect(m_templateComponentList, &QListWidget::currentRowChanged, this, [this](int row) {
        if (m_templateCanvas) m_templateCanvas->setSelectedIndex(row);
        refreshInlineTemplateEditor();
    });
    connect(addText, &QPushButton::clicked, this, [this] { addInlineTemplateComponent(QStringLiteral("text")); });
    connect(addImage, &QPushButton::clicked, this, [this] { selectInlineGalleryAsset(); });
    connect(addLastRX, &QPushButton::clicked, this, [this] { addInlineTemplateComponent(QStringLiteral("last-rx-image")); });
    connect(clearBg, &QPushButton::clicked, this, [this] {
        m_settings.remove(QStringLiteral("templateBackgroundAssetId"));
        saveSettings(m_settings);
        m_templateImageCache.clear();
        refreshTemplateCanvas();
    });
    connect(deleteComponent, &QPushButton::clicked, this, [this] {
        const int row = m_templateComponentList ? m_templateComponentList->currentRow() : -1;
        QJsonObject tpl = selectedTemplate();
        QJsonArray components = tpl.value(QStringLiteral("components")).toArray();
        if (row < 0 || row >= components.size()) return;
        components.removeAt(row);
        tpl.insert(QStringLiteral("components"), components);
        const QString id = tpl.value(QStringLiteral("id")).toString();
        for (int i=0;i<m_templates.size();++i) if(m_templates.at(i).toObject().value(QStringLiteral("id")).toString()==id){m_templates.replace(i,tpl);break;}
        refreshTemplateComponentList();
        refreshTemplateCanvas();
    });
    auto propertyChanged = [this] { applyInlineTemplateProperties(); };
    connect(m_componentTextEdit, &QTextEdit::textChanged, this, propertyChanged);
    connect(m_componentAssetCombo, QOverload<int>::of(&QComboBox::currentIndexChanged), this, propertyChanged);
    connect(m_componentScaleSlider, &QSlider::valueChanged, this, propertyChanged);
    connect(m_componentFontSlider, &QSlider::valueChanged, this, propertyChanged);
    connect(m_componentOutlineSlider, &QSlider::valueChanged, this, propertyChanged);
    connect(m_componentBoldCheck, &QCheckBox::toggled, this, propertyChanged);
    connect(m_componentVisibleCheck, &QCheckBox::toggled, this, propertyChanged);
    connect(m_componentOutlineCheck, &QCheckBox::toggled, this, propertyChanged);
    connect(m_componentColorButton, &QPushButton::clicked, this, [this] {
        const auto component = selectedTemplate().value(QStringLiteral("components")).toArray().at(m_templateComponentList->currentRow()).toObject();
        QColor color(valueOr(component, QStringLiteral("color"), QStringLiteral("#ffffff")));
        color = QColorDialog::getColor(color, this, uiText(QStringLiteral("文字颜色")));
        if (!color.isValid()) return;
        m_componentColorButton->setProperty("color", color.name(QColor::HexRgb));
        m_componentColorButton->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(color.name(), color.lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
        applyInlineTemplateProperties();
    });
    connect(m_componentOutlineColorButton, &QPushButton::clicked, this, [this] {
        const auto component = selectedTemplate().value(QStringLiteral("components")).toArray().at(m_templateComponentList->currentRow()).toObject();
        QColor color(valueOr(component, QStringLiteral("outlineColor"), QStringLiteral("#000000")));
        color = QColorDialog::getColor(color, this, uiText(QStringLiteral("描边颜色")));
        if (!color.isValid()) return;
        m_componentOutlineColorButton->setProperty("color", color.name(QColor::HexRgb));
        m_componentOutlineColorButton->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(color.name(), color.lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
        applyInlineTemplateProperties();
    });
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
    m_txMetrics = new QLabel(uiText(QStringLiteral("预计 %1 · 已发射 %2 · 剩余 %3 · 数据帧 %4 / %5"))
        .arg(QStringLiteral("00:00:00"), QStringLiteral("00:00:00"), QStringLiteral("00:00:00"), QStringLiteral("0"), QStringLiteral("0")), txPane);
    m_txMetrics->setObjectName(QStringLiteral("txMetrics"));
    m_txMetrics->setWordWrap(true);
    m_txMetrics->setMinimumHeight(30);
    m_txMetrics->setStyleSheet(QStringLiteral("font-size:8pt; color:#586b77; padding:2px 1px; background:transparent; border:0;"));
    txLayout->addWidget(m_txMetrics);
    auto *txSpectrumTitle = new QHBoxLayout();
    txSpectrumTitle->addWidget(new QLabel(uiText(QStringLiteral("TX 瀑布图")), txPane));
    txSpectrumTitle->addStretch();
    txLayout->addLayout(txSpectrumTitle);
    m_txSpectrumStatus = new QLabel(uiText(QStringLiteral("等待 TX 音频")), txPane);
    m_txSpectrumStatus->setStyleSheet(QStringLiteral("font-size:8pt; color:#77858f;"));
    m_txSpectrumStatus->hide();
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
    tabs->addTab(workbench, uiText(QStringLiteral("工作台")));

    // Album cards replace the data table. Each card keeps the image itself as
    // the primary navigation target, just like the original grid view.
    auto *albumPage = new QWidget(tabs);
    auto *albumLayout = new QVBoxLayout(albumPage);
    albumLayout->setContentsMargins(10, 9, 10, 8);
    albumLayout->setSpacing(8);
    auto *albumToolbar = new QHBoxLayout();
    auto *albumTitle = new QLabel(uiText(QStringLiteral("RX / TX HISTORY   历史相册")), albumPage);
    albumTitle->setStyleSheet(QStringLiteral("font-size:11pt; font-weight:700; color:#344b58;"));
    albumToolbar->addWidget(albumTitle);
    albumToolbar->addStretch();
    m_albumFilter = makeCombo(albumPage, {uiText(QStringLiteral("全部图像")), uiText(QStringLiteral("仅接收 RX")), uiText(QStringLiteral("仅发送 TX"))});
    m_albumFilter->setMinimumWidth(125);
    auto *refresh = new QPushButton(uiText(QStringLiteral("刷新")), albumPage);
    auto *importImage = new QPushButton(uiText(QStringLiteral("导入图库图片")), albumPage);
    auto *exportAll = new QPushButton(uiText(QStringLiteral("导出相册到文件夹")), albumPage);
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
    auto *albumHint = new QLabel(uiText(QStringLiteral("点击缩略图预览；每张图像可单独导出或删除记录。")), albumPage);
    albumHint->setStyleSheet(QStringLiteral("font-size:8pt; color:#7d8991;"));
    albumLayout->addWidget(albumHint);
    connect(refresh, &QPushButton::clicked, this, [this] { refreshAlbum(); refreshGallery(); });
    connect(importImage, &QPushButton::clicked, this, [this] { importGalleryImage(); });
    connect(exportAll, &QPushButton::clicked, this, [this] { exportAlbum(); });
    connect(m_albumFilter, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int) { refreshAlbum(); });
    tabs->addTab(albumPage, uiText(QStringLiteral("相册")));

    root->addWidget(tabs, 1);
    setCentralWidget(central);
    statusBar()->showMessage(uiText(QStringLiteral("Go 后端通过本机回环 RPC 连接；RX / TX 频谱分别显示。")));

    // Hidden log storage preserves diagnostic messages without adding a
    // permanent third pane to the operator's workbench.
    m_log = new QTextEdit(this);
    m_log->hide();
}

void MainWindow::loadInitialState() {
    QString error;
    const auto profiles = m_backend->callSync(QStringLiteral("GetProfiles"), {}, &error);
    if (!error.isEmpty()) {
        appendLog(uiText(QStringLiteral("读取模式列表失败：")) + error);
        m_audioStatus->setText(uiText(QStringLiteral("模式列表不可用")));
        statusBar()->showMessage(uiText(QStringLiteral("初始化失败：%1")).arg(error));
        return;
    }
    m_profiles = profiles.toArray();
    m_rxProfile->clear();
    m_rxProfile->addItem(uiText(QStringLiteral("自动识别")), QStringLiteral("auto"));
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
        appendLog(uiText(QStringLiteral("读取设置失败：")) + error);
        statusBar()->showMessage(uiText(QStringLiteral("设置读取失败，使用默认值：%1")).arg(error));
    }
    m_settings = settings;
    QJsonObject waterfall = settings.value(QStringLiteral("waterfall")).toObject();
    if (waterfall.isEmpty()) {
        waterfall.insert(QStringLiteral("minFrequencyHz"), 0);
        waterfall.insert(QStringLiteral("maxFrequencyHz"), 5000);
        waterfall.insert(QStringLiteral("minDbfs"), -110);
        waterfall.insert(QStringLiteral("maxDbfs"), -20);
        waterfall.insert(QStringLiteral("rowsPerSecond"), 15);
        waterfall.insert(QStringLiteral("historySeconds"), 20);
        waterfall.insert(QStringLiteral("backgroundColor"), QStringLiteral("#07111b"));
        waterfall.insert(QStringLiteral("lowColor"), QStringLiteral("#153a78"));
        waterfall.insert(QStringLiteral("midColor"), QStringLiteral("#168fae"));
        waterfall.insert(QStringLiteral("highColor"), QStringLiteral("#39c6b0"));
        waterfall.insert(QStringLiteral("peakColor"), QStringLiteral("#f7d45b"));
        m_settings.insert(QStringLiteral("waterfall"), waterfall);
    }
    if (m_rxSpectrum) m_rxSpectrum->setDisplaySettings(waterfall);
    if (m_txSpectrum) m_txSpectrum->setDisplaySettings(waterfall);
    const QString locale = valueOr(settings, QStringLiteral("language"), QStringLiteral("zh-CN"));
    if (m_backend && m_backend->isRunning()) {
        QString localeError;
        m_localeDictionary = m_backend->callSync(QStringLiteral("GetLocaleDictionary"), QJsonArray{locale}, &localeError, 5000).toObject();
    }
    setUiLocale(locale);
    if (m_languageSelect) {
        const QSignalBlocker blocker(m_languageSelect);
        m_languageSelect->setCurrentIndex(locale == QStringLiteral("en") ? 1 : locale.startsWith(QStringLiteral("ja")) ? 2 : 0);
    }
    translateWidgetTree(this);
    m_callsign->setText(valueOr(settings, QStringLiteral("callsign"), QStringLiteral("N0CALL")));
    m_locator->setText(valueOr(settings, QStringLiteral("locator"), QStringLiteral("JJ00")));
    m_ax25SSID->setValue(settings.value(QStringLiteral("ax25Ssid")).toInt(0));
    connect(m_ax25SSID, QOverload<int>::of(&QSpinBox::valueChanged), this, [this](int value) {
        m_settings.insert(QStringLiteral("ax25Ssid"), value);
        saveSettings(m_settings);
    });
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
            if (settingKey == QStringLiteral("txProfile")) updateTXProfileControls();
            saveSettings(m_settings);
        });
    };
    addProfileButton(m_rxProfileButtons, m_rxProfile, QStringLiteral("rxProfile"), QStringLiteral("auto"),
                     uiText(QStringLiteral("自动识别")), m_rxProfile->currentData().toString() == QStringLiteral("auto"));
    for (const auto &value : m_profiles) {
        const auto profile = value.toObject();
        const QString id = profile.value(QStringLiteral("id")).toString();
        const QString label = valueOr(profile, QStringLiteral("name"), id);
        addProfileButton(m_rxProfileButtons, m_rxProfile, QStringLiteral("rxProfile"), id, label, m_rxProfile->currentData().toString() == id);
        addProfileButton(m_txProfileButtons, m_txProfile, QStringLiteral("txProfile"), id, label, m_txProfile->currentData().toString() == id);
    }
    updateTXProfileControls();
    clearHisCallForCQTemplate();
    const QString quality = valueOr(settings, QStringLiteral("quality"), QStringLiteral("medium"));
    updateRXToggle();
    updateTXToggle();
    m_quality->setCurrentIndex(quality == QStringLiteral("low") ? 0 : quality == QStringLiteral("high") ? 2 : 1);
    m_repeatIntervalSeconds = m_repeatInterval->value();
    m_repeatInterval->setEnabled(!m_repeat->isChecked());
    connect(m_quality, QOverload<int>::of(&QComboBox::currentIndexChanged), this, [this](int index) {
        m_settings.insert(QStringLiteral("quality"),index==0?QStringLiteral("low"):index==2?QStringLiteral("high"):QStringLiteral("medium"));
        saveSettings(m_settings);
        refreshTemplateCanvas();
    });
    connect(m_repeatInterval, QOverload<int>::of(&QSpinBox::valueChanged), this, [this](int value) { if (!m_repeat->isChecked()) m_repeatIntervalSeconds = value; });
    refreshTemplates();
    refreshGallery();
    refreshAlbum();
    refreshTemplateCanvas();
    refreshInlineTemplateEditor();
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
            m_lastRXPreviewIsGIF = value.toString().startsWith(QStringLiteral("data:image/gif"), Qt::CaseInsensitive);
            showImage(value.toString(), m_rxPreview);
            refreshInlineTemplateEditor();
            refreshTemplateCanvas();
        } else if (!ok) {
            appendLog(uiText(QStringLiteral("刷新接收图像预览失败：")) + error);
        }
        if (m_rxPreviewRefreshQueued) refreshLatestRXPreview();
    });
}

void MainWindow::refreshStatus() {
    if (m_statusRequestPending || !m_backend || !m_backend->isRunning()) return;
    m_statusRequestPending = true;
    m_backend->call(QStringLiteral("GetAudioState"), {}, [this](bool ok, const QJsonValue &value, const QString &error) {
        m_statusRequestPending = false;
        if (!ok) {
            m_audioConnected = false;
            updateRXToggle();
            m_audioStatus->setText(uiText(QStringLiteral("音频后端异常")));
            m_audioStatus->setToolTip(error);
            m_audioStatus->setStyleSheet(QStringLiteral("background:#f8eeee; border:1px solid #d7b2b7; padding:6px 10px; color:#98545c;"));
            return;
        }
        const QJsonObject state = value.toObject();
        const bool connected = state.value(QStringLiteral("connected")).toBool();
        m_audioConnected = connected;
        m_rxActive = state.value(QStringLiteral("rxActive")).toBool();
        m_txActive = state.value(QStringLiteral("txActive")).toBool();
        updateRepeatControls();
        const int rate = state.value(QStringLiteral("sampleRate")).toInt();
        m_audioStatus->setText(uiText(QStringLiteral("音频：%1 · %2 Hz · RX %3 / TX %4"))
            .arg(connected ? uiText(QStringLiteral("已连接")) : uiText(QStringLiteral("未初始化")))
            .arg(rate)
            .arg(m_rxActive ? uiText(QStringLiteral("运行")) : uiText(QStringLiteral("待机")))
            .arg(m_txActive ? uiText(QStringLiteral("运行")) : uiText(QStringLiteral("待机"))));
        m_audioStatus->setStyleSheet(connected
            ? QStringLiteral("background:#e4f0e9; border:1px solid #a9c8b5; padding:6px 10px; color:#47735a;")
            : QStringLiteral("background:#f7eddb; border:1px solid #ddc99f; padding:6px 10px; color:#8a6732;"));
        m_audioStatus->setToolTip(state.value(QStringLiteral("error")).toString());
        updateRXToggle();
        updateTXToggle();
    });
}

void MainWindow::refreshTemplates() {
    QString error; const auto result=m_backend->callSync(QStringLiteral("GetTemplates"),{},&error);
    if (!error.isEmpty()) { appendLog(error); return; }
    m_templates = result.toArray();
    if (!m_settings.value(QStringLiteral("imageOutlineMigratedV1")).toBool()) {
        bool changed = false;
        for (int i = 0; i < m_templates.size(); ++i) {
            QJsonObject item = m_templates.at(i).toObject();
            QJsonArray components = item.value(QStringLiteral("components")).toArray();
            for (int j = 0; j < components.size(); ++j) {
                QJsonObject component = components.at(j).toObject();
                const QString type = component.value(QStringLiteral("type")).toString();
                if (type != QStringLiteral("image") && type != QStringLiteral("last-rx-image")) continue;
                if (!component.value(QStringLiteral("outline")).toBool(false)) {
                    component.insert(QStringLiteral("outline"), true);
                    changed = true;
                }
                if (!component.contains(QStringLiteral("outlineWidth"))) {
                    component.insert(QStringLiteral("outlineWidth"), 2.0);
                    changed = true;
                }
                if (!component.contains(QStringLiteral("outlineColor"))) {
                    component.insert(QStringLiteral("outlineColor"), QStringLiteral("#000000"));
                    changed = true;
                }
                components.replace(j, component);
            }
            item.insert(QStringLiteral("components"), components);
            m_templates.replace(i, item);
        }
        bool migrationSaved = true;
        if (changed) {
            QString migrationError;
            m_backend->callSync(QStringLiteral("SaveTemplates"), QJsonArray{m_templates}, &migrationError, 15000);
            if (!migrationError.isEmpty()) {
                appendLog(QStringLiteral("Image outline migration failed: ") + migrationError);
                migrationSaved = false;
            }
        }
        if (migrationSaved) {
            m_settings.insert(QStringLiteral("imageOutlineMigratedV1"), true);
            QString settingsError;
            m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &settingsError, 15000);
            if (!settingsError.isEmpty()) appendLog(QStringLiteral("Unable to save image-outline migration state: ") + settingsError);
        }
    }
    const QString current=m_template->currentData().toString(); m_template->clear();
    for(const auto &item:m_templates){const auto tpl=item.toObject();m_template->addItem(tpl.value(QStringLiteral("name")).toString(),tpl.value(QStringLiteral("id")).toString());}
    int index = m_template->findData(current);
    if (index < 0 && m_template->count() > 0) index = 0;
    if (index >= 0) m_template->setCurrentIndex(index);
    if (m_templateName) m_templateName->setText(m_template->currentText());
    refreshTemplateComponentList();
    refreshTemplateCanvas();
}

void MainWindow::refreshTemplateComponentList() {
    const QJsonObject selected = selectedTemplate();
    const QJsonArray components = selected.value(QStringLiteral("components")).toArray();
    if (m_templateComponentsSummary) {
        m_templateComponentsSummary->setText(uiText(QStringLiteral("%1 · %2 项"))
            .arg(selected.value(QStringLiteral("name")).toString(), QString::number(components.size())));
    }
    if (!m_templateComponentList) return;
    const QString selectedComponentId = m_templateComponentList->currentItem()
        ? m_templateComponentList->currentItem()->data(Qt::UserRole).toString() : QString();
    const QSignalBlocker blocker(m_templateComponentList);
    m_templateComponentList->clear();
    int preferredRow = -1;
    for (int i = 0; i < components.size(); ++i) {
        const QJsonObject component = components.at(i).toObject();
        const QString type = component.value(QStringLiteral("type")).toString();
        const QString id = component.value(QStringLiteral("id")).toString();
        QString label = type == QStringLiteral("text")
            ? QStringLiteral("T · %1").arg(valueOr(component, QStringLiteral("text"), uiText(QStringLiteral("文字"))))
            : type == QStringLiteral("image") ? QStringLiteral("IMG · %1").arg(valueOr(component, QStringLiteral("assetName"), valueOr(component, QStringLiteral("assetId"), uiText(QStringLiteral("图库图像")))))
            : uiText(QStringLiteral("RX · 上次接收图像"));
        auto *item = new QListWidgetItem(label.left(34), m_templateComponentList);
        item->setData(Qt::UserRole, id);
        if (id == selectedComponentId) preferredRow = i;
    }
    if (preferredRow < 0 && !components.isEmpty()) preferredRow = 0;
    m_templateComponentList->setCurrentRow(preferredRow);
    refreshInlineTemplateEditor();
}

void MainWindow::refreshTemplateCanvas() {
    if (!m_templateCanvas) return;
    const QJsonObject selected = selectedTemplate();
    QJsonArray components = selected.value(QStringLiteral("components")).toArray();
    for (int i = 0; i < components.size(); ++i) {
        QJsonObject component = components.at(i).toObject();
        const QString type = component.value(QStringLiteral("type")).toString();
        const bool gif = type == QStringLiteral("image")
            ? isGifGalleryAsset(m_gallery, component.value(QStringLiteral("assetId")).toString())
            : type == QStringLiteral("last-rx-image") && m_lastRXPreviewIsGIF;
        component.insert(QStringLiteral("isGIF"), gif);
        components.replace(i, component);
    }
    QImage background(640, 480, QImage::Format_ARGB32_Premultiplied);
    background.fill(QColor(QStringLiteral("#17232d")));
    const QString backgroundId = valueOr(m_settings, QStringLiteral("templateBackgroundAssetId"));
    if (!backgroundId.isEmpty() && m_backend && m_backend->isRunning()) {
        const QString cacheKey = QStringLiteral("background:%1").arg(backgroundId);
        if (!m_templateImageCache.contains(cacheKey)) {
            QString error;
            const QString data = m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{backgroundId}, &error, 5000).toString();
            if (error.isEmpty()) m_templateImageCache.insert(cacheKey, imageFromDataUrl(data));
        }
        const QImage source = m_templateImageCache.value(cacheKey);
        if (!source.isNull()) {
            QPainter painter(&background);
            const qreal factor = qMax(background.width() / qreal(source.width()), background.height() / qreal(source.height()));
            const QSizeF scaled(source.width() * factor, source.height() * factor);
            painter.drawImage(QRectF((background.width() - scaled.width()) / 2.0,
                                     (background.height() - scaled.height()) / 2.0,
                                     scaled.width(), scaled.height()), source);
        }
    }
    QHash<QString, QImage> images;
    if (m_backend && m_backend->isRunning()) {
        for (const QJsonValue &value : components) {
            const QJsonObject component = value.toObject();
            const QString id = component.value(QStringLiteral("id")).toString();
            const QString type = component.value(QStringLiteral("type")).toString();
            if (id.isEmpty() || (type != QStringLiteral("image") && type != QStringLiteral("last-rx-image"))) continue;
            const QString cacheKey = type == QStringLiteral("image")
                ? QStringLiteral("gallery:%1").arg(component.value(QStringLiteral("assetId")).toString())
                : QStringLiteral("last-rx");
            if (!m_templateImageCache.contains(cacheKey)) {
                QString error;
                const QString data = type == QStringLiteral("image")
                    ? m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{component.value(QStringLiteral("assetId")).toString()}, &error, 5000).toString()
                    : m_backend->callSync(QStringLiteral("GetLatestRXImageData"), {}, &error, 5000).toString();
                if (error.isEmpty()) m_templateImageCache.insert(cacheKey, imageFromDataUrl(data));
            }
            images.insert(id, m_templateImageCache.value(cacheKey));
        }
    }
    m_templateCanvas->setDocument(components, background, images,
        [this](const QString &text) { return resolveTemplateText(text); });
    if (m_templateComponentList && m_templateComponentList->currentRow() >= 0)
        m_templateCanvas->setSelectedIndex(m_templateComponentList->currentRow());
}

void MainWindow::refreshInlineTemplateEditor() {
    if (!m_templateComponentList) return;
    const int row = m_templateComponentList->currentRow();
    const QJsonArray components = selectedTemplate().value(QStringLiteral("components")).toArray();
    const bool valid = row >= 0 && row < components.size();
    const QJsonObject component = valid ? components.at(row).toObject() : QJsonObject();
    const QString type = component.value(QStringLiteral("type")).toString();
    const bool isText = valid && type == QStringLiteral("text");
    const bool isImage = valid && type == QStringLiteral("image");
    const bool isImageLayer = isImage || (valid && type == QStringLiteral("last-rx-image"));
    const bool isGIFImage = isImage
        ? isGifGalleryAsset(m_gallery, component.value(QStringLiteral("assetId")).toString())
        : valid && type == QStringLiteral("last-rx-image") && m_lastRXPreviewIsGIF;
    const bool showOutline = valid && !isGIFImage;

    const QSignalBlocker b1(m_componentTextEdit), b2(m_componentAssetCombo), b3(m_componentScaleSlider),
        b4(m_componentFontSlider), b5(m_componentOutlineSlider), b6(m_componentBoldCheck),
        b7(m_componentVisibleCheck), b8(m_componentOutlineCheck);
    if (m_componentTextEdit) {
        m_componentTextEdit->setPlainText(component.value(QStringLiteral("text")).toString());
        m_componentTextEdit->setEnabled(isText);
        m_componentTextEdit->setVisible(isText);
    }
    if (m_componentTextLabel) m_componentTextLabel->setVisible(isText);

    if (m_componentAssetCombo) {
        m_componentAssetCombo->clear();
        for (const QJsonValue &assetValue : m_gallery) {
            const QJsonObject asset = assetValue.toObject();
            m_componentAssetCombo->addItem(valueOr(asset, QStringLiteral("name"), asset.value(QStringLiteral("id")).toString()), asset.value(QStringLiteral("id")).toString());
        }
        const int assetIndex = m_componentAssetCombo->findData(component.value(QStringLiteral("assetId")).toString());
        if (assetIndex >= 0) m_componentAssetCombo->setCurrentIndex(assetIndex);
        m_componentAssetCombo->setEnabled(isImage);
        m_componentAssetCombo->setVisible(isImage);
    }
    if (m_componentAssetLabel) m_componentAssetLabel->setVisible(isImage);

    if (m_componentScaleSlider) {
        m_componentScaleSlider->setValue(qBound(5, component.value(QStringLiteral("scale")).toInt(35), 100));
        m_componentScaleSlider->setEnabled(isImageLayer);
        m_componentScaleSlider->setVisible(isImageLayer);
    }
    if (m_componentScaleLabel) m_componentScaleLabel->setVisible(isImageLayer);

    if (m_componentFontSlider) {
        m_componentFontSlider->setValue(qBound(6, component.value(QStringLiteral("fontSize")).toInt(28), 160));
        m_componentFontSlider->setEnabled(isText);
        m_componentFontSlider->setVisible(isText);
    }
    if (m_componentFontLabel) m_componentFontLabel->setVisible(isText);

    if (m_componentOutlineSlider) {
        m_componentOutlineSlider->setValue(qBound(0, component.value(QStringLiteral("outlineWidth")).toInt(2), 20));
        m_componentOutlineSlider->setEnabled(showOutline);
        m_componentOutlineSlider->setVisible(showOutline);
    }
    if (m_componentOutlineLabel) m_componentOutlineLabel->setVisible(showOutline);

    if (m_componentBoldCheck) {
        m_componentBoldCheck->setChecked(component.value(QStringLiteral("bold")).toBool());
        m_componentBoldCheck->setEnabled(isText);
        m_componentBoldCheck->setVisible(isText);
    }
    if (m_componentVisibleCheck) {
        m_componentVisibleCheck->setChecked(component.value(QStringLiteral("visible")).toBool(true));
        m_componentVisibleCheck->setEnabled(valid);
        m_componentVisibleCheck->setVisible(valid);
    }
    if (m_componentOutlineCheck) {
        m_componentOutlineCheck->setChecked(showOutline && component.value(QStringLiteral("outline")).toBool(true));
        m_componentOutlineCheck->setEnabled(showOutline);
        m_componentOutlineCheck->setVisible(showOutline);
    }

    const QString color = valueOr(component, QStringLiteral("color"), QStringLiteral("#ffffff"));
    const QString outline = valueOr(component, QStringLiteral("outlineColor"), QStringLiteral("#000000"));
    if (m_componentColorButton) {
        m_componentColorButton->setEnabled(isText);
        m_componentColorButton->setVisible(isText);
        m_componentColorButton->setProperty("color", color);
        m_componentColorButton->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(color, QColor(color).lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
    }
    if (m_componentOutlineColorButton) {
        m_componentOutlineColorButton->setEnabled(showOutline);
        m_componentOutlineColorButton->setVisible(showOutline);
        m_componentOutlineColorButton->setProperty("color", outline);
        m_componentOutlineColorButton->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(outline, QColor(outline).lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
    }
}

void MainWindow::applyInlineTemplateProperties() {
    const int row = m_templateComponentList ? m_templateComponentList->currentRow() : -1;
    QJsonObject tpl = selectedTemplate();
    QJsonArray components = tpl.value(QStringLiteral("components")).toArray();
    if (row < 0 || row >= components.size()) return;
    QJsonObject component = components.at(row).toObject();
    const QString type = component.value(QStringLiteral("type")).toString();
    if (type == QStringLiteral("text")) {
        component.insert(QStringLiteral("text"), m_componentTextEdit->toPlainText());
        component.insert(QStringLiteral("fontSize"), m_componentFontSlider->value());
        component.insert(QStringLiteral("bold"), m_componentBoldCheck->isChecked());
        component.insert(QStringLiteral("color"), m_componentColorButton->property("color").toString());
    } else {
        component.insert(QStringLiteral("scale"), m_componentScaleSlider->value());
        if (type == QStringLiteral("image")) component.insert(QStringLiteral("assetId"), m_componentAssetCombo->currentData().toString());
    }
    const bool isGIFImage = type == QStringLiteral("image")
        ? isGifGalleryAsset(m_gallery, component.value(QStringLiteral("assetId")).toString())
        : type == QStringLiteral("last-rx-image") && m_lastRXPreviewIsGIF;
    component.insert(QStringLiteral("isGIF"), isGIFImage);
    if (isGIFImage) {
        component.remove(QStringLiteral("outline"));
        component.remove(QStringLiteral("outlineWidth"));
        component.remove(QStringLiteral("outlineColor"));
    } else {
        component.insert(QStringLiteral("outlineWidth"), m_componentOutlineSlider->value());
        component.insert(QStringLiteral("outlineColor"), m_componentOutlineColorButton->property("color").toString());
        component.insert(QStringLiteral("outline"), m_componentOutlineCheck->isChecked());
    }
    component.insert(QStringLiteral("visible"), m_componentVisibleCheck->isChecked());
    components.replace(row, component);
    tpl.insert(QStringLiteral("components"), components);
    const QString id = tpl.value(QStringLiteral("id")).toString();
    for (int i=0;i<m_templates.size();++i) if(m_templates.at(i).toObject().value(QStringLiteral("id")).toString()==id){m_templates.replace(i,tpl);break;}
    if (m_templateComponentList->currentItem()) {
        const QString label = type == QStringLiteral("text")
            ? QStringLiteral("T · %1").arg(valueOr(component, QStringLiteral("text"), uiText(QStringLiteral("文字"))))
            : type == QStringLiteral("image") ? QStringLiteral("IMG · %1").arg(m_componentAssetCombo->currentText().isEmpty() ? valueOr(component, QStringLiteral("assetId"), uiText(QStringLiteral("图库图像"))) : m_componentAssetCombo->currentText())
            : uiText(QStringLiteral("RX · 上次接收图像"));
        m_templateComponentList->currentItem()->setText(label.left(34));
    }
    refreshTemplateCanvas();
}

void MainWindow::addInlineTemplateComponent(const QString &type, const QString &assetId) {
    QJsonObject tpl = selectedTemplate();
    if (tpl.isEmpty()) { statusBar()->showMessage(uiText(QStringLiteral("请先选择发射模板")), 4000); return; }
    QJsonArray components = tpl.value(QStringLiteral("components")).toArray();
    if (components.size() >= 64) { statusBar()->showMessage(uiText(QStringLiteral("单个模板最多支持 64 个组件")), 5000); return; }
    if (type == QStringLiteral("image") && assetId.isEmpty()) return;
    QJsonObject component;
    const QString id = QUuid::createUuid().toString(QUuid::WithoutBraces);
    component.insert(QStringLiteral("id"), id);
    component.insert(QStringLiteral("type"), type);
    component.insert(QStringLiteral("x"), type == QStringLiteral("last-rx-image") ? 58.0 : 8.0);
    component.insert(QStringLiteral("y"), 42.0);
    component.insert(QStringLiteral("scale"), 35.0);
    component.insert(QStringLiteral("fontSize"), 28.0);
    component.insert(QStringLiteral("bold"), true);
    component.insert(QStringLiteral("color"), QStringLiteral("#ffffff"));
    component.insert(QStringLiteral("visible"), true);
    component.insert(QStringLiteral("outline"), true);
    component.insert(QStringLiteral("outlineWidth"), 2.0);
    component.insert(QStringLiteral("outlineColor"), QStringLiteral("#000000"));
    if (type == QStringLiteral("text")) component.insert(QStringLiteral("text"), uiText(QStringLiteral("新文本")));
    if (type == QStringLiteral("image")) {
        component.insert(QStringLiteral("assetId"), assetId);
        const bool gif = isGifGalleryAsset(m_gallery, assetId);
        component.insert(QStringLiteral("isGIF"), gif);
        if (gif) {
            component.remove(QStringLiteral("outline"));
            component.remove(QStringLiteral("outlineWidth"));
            component.remove(QStringLiteral("outlineColor"));
        }
    }
    components.append(component);
    tpl.insert(QStringLiteral("components"), components);
    const QString templateId = tpl.value(QStringLiteral("id")).toString();
    for (int i=0;i<m_templates.size();++i) if(m_templates.at(i).toObject().value(QStringLiteral("id")).toString()==templateId){m_templates.replace(i,tpl);break;}
    m_templateImageCache.clear();
    refreshTemplateComponentList();
    if (m_templateComponentList) m_templateComponentList->setCurrentRow(components.size()-1);
    refreshTemplateCanvas();
    refreshInlineTemplateEditor();
}

void MainWindow::selectInlineGalleryAsset() {
    refreshGallery();
    QDialog dialog(this);
    dialog.setWindowTitle(uiText(QStringLiteral("图像图库")));
    dialog.resize(760, 560);
    auto *root = new QVBoxLayout(&dialog);
    auto *toolbar = new QHBoxLayout();
    auto *title = new QLabel(uiText(QStringLiteral("选择图像资源")), &dialog);
    title->setStyleSheet(QStringLiteral("font-weight:700; color:#344b58;"));
    auto *importButton = new QPushButton(uiText(QStringLiteral("导入图片…")), &dialog);
    toolbar->addWidget(title);
    toolbar->addStretch();
    toolbar->addWidget(importButton);
    root->addLayout(toolbar);
    auto *scroll = new QScrollArea(&dialog);
    scroll->setWidgetResizable(true);
    auto *host = new QWidget(scroll);
    auto *grid = new QGridLayout(host);
    grid->setContentsMargins(6, 6, 6, 6);
    grid->setSpacing(8);
    grid->setAlignment(Qt::AlignTop | Qt::AlignLeft);
    scroll->setWidget(host);
    root->addWidget(scroll, 1);
    auto *footer = new QLabel(uiText(QStringLiteral("选择‘设为底图’设置共享背景；选择‘添加图层’将图片放入当前 TX 模板。")), &dialog);
    footer->setStyleSheet(QStringLiteral("font-size:8pt; color:#7d8991;"));
    root->addWidget(footer);
    connect(importButton, &QPushButton::clicked, &dialog, [this, &dialog]() {
        importGalleryImage();
        refreshGallery();
        dialog.accept();
    });

    const int columns = 3;
    int row = 0, column = 0;
    for (const QJsonValue &value : m_gallery) {
        const QJsonObject asset = value.toObject();
        const QString id = asset.value(QStringLiteral("id")).toString();
        if (id.isEmpty()) continue;
        auto *card = new QFrame(host);
        card->setMinimumWidth(205);
        card->setMaximumWidth(245);
        card->setStyleSheet(QStringLiteral("QFrame { background:#f8f9fa; border:1px solid #c4cbd0; }"));
        auto *cardLayout = new QVBoxLayout(card);
        cardLayout->setContentsMargins(6, 6, 6, 6);
        cardLayout->setSpacing(5);
        auto *preview = new QLabel(card);
        preview->setAlignment(Qt::AlignCenter);
        preview->setMinimumHeight(118);
        preview->setStyleSheet(QStringLiteral("background:#ffffff; border:1px solid #d0d6da; color:#8b959c;"));
        preview->setText(uiText(QStringLiteral("加载预览…")));
        cardLayout->addWidget(preview);
        QString imageError;
        const QString imageData = m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{id}, &imageError, 8000).toString();
        const QImage image = imageFromDataUrl(imageData);
        if (!image.isNull()) preview->setPixmap(QPixmap::fromImage(image).scaled(220, 118, Qt::KeepAspectRatio, Qt::SmoothTransformation));
        else preview->setText(uiText(QStringLiteral("预览不可用")));
        auto *name = new QLabel(valueOr(asset, QStringLiteral("name"), id), card);
        name->setWordWrap(true);
        name->setStyleSheet(QStringLiteral("font-size:9pt; font-weight:600; color:#44535e;"));
        cardLayout->addWidget(name);
        auto *size = new QLabel(QStringLiteral("%1 × %2").arg(asset.value(QStringLiteral("width")).toInt()).arg(asset.value(QStringLiteral("height")).toInt()), card);
        size->setStyleSheet(QStringLiteral("font-size:8pt; color:#89939b;"));
        cardLayout->addWidget(size);
        auto *actions = new QHBoxLayout();
        auto *setBackground = new QPushButton(uiText(QStringLiteral("设为底图")), card);
        auto *addLayer = new QPushButton(uiText(QStringLiteral("添加图层")), card);
        actions->addWidget(setBackground);
        actions->addWidget(addLayer);
        cardLayout->addLayout(actions);
        connect(setBackground, &QPushButton::clicked, &dialog, [this, id, &dialog]() {
            m_settings.insert(QStringLiteral("templateBackgroundAssetId"), id);
            saveSettings(m_settings);
            m_templateImageCache.clear();
            refreshTemplateCanvas();
            QString renderError;
            const QImage rendered = renderTemplate(&renderError);
            if (!rendered.isNull() && m_txPreview) m_txPreview->setSourceImage(rendered);
            statusBar()->showMessage(uiText(QStringLiteral("模板共享底图已更新")), 4000);
            dialog.accept();
        });
        connect(addLayer, &QPushButton::clicked, &dialog, [this, id, &dialog]() {
            addInlineTemplateComponent(QStringLiteral("image"), id);
            dialog.accept();
        });
        grid->addWidget(card, row, column);
        ++column;
        if (column >= columns) { column = 0; ++row; }
    }
    if (m_gallery.isEmpty()) {
        auto *empty = new QLabel(uiText(QStringLiteral("图库暂无图片。请使用右上方‘导入图片’添加图像资源。")), host);
        empty->setAlignment(Qt::AlignCenter);
        empty->setMinimumHeight(180);
        grid->addWidget(empty, 0, 0, 1, columns);
    }
    dialog.exec();
}

void MainWindow::updateTXProfileControls() {
    const bool ax25 = m_txProfile && m_txProfile->currentData().toString() == QStringLiteral("ssdv-ax25");
    if (m_ax25SSIDField) m_ax25SSIDField->setVisible(ax25);
    if (m_ax25SSID) m_ax25SSID->setEnabled(ax25);
    clearHisCallForCQTemplate();
}

void MainWindow::clearHisCallForCQTemplate() {
    if (!m_hisCall || !m_txProfile || m_txProfile->currentData().toString() != QStringLiteral("ssdv-ax25")) return;
    QString name = selectedTemplate().value(QStringLiteral("name")).toString().trimmed();
    name.remove(QRegularExpression(QStringLiteral("^\\d+[\\s._-]*")));
    if (name.compare(QStringLiteral("CQ"), Qt::CaseInsensitive) != 0) return;
    if (!m_hisCall->text().isEmpty()) m_hisCall->clear();
    m_hisCallManuallyEdited = false;
}

void MainWindow::updateRXToggle() {
    if (!m_rxToggle) return;
    m_rxToggle->setText(m_rxActive
        ? m_localeDictionary.value(QStringLiteral("audio.rxOn")).toString(uiText(QStringLiteral("停止 RX")))
        : m_localeDictionary.value(QStringLiteral("audio.rxOff")).toString(uiText(QStringLiteral("启动 RX"))));
    m_rxToggle->setEnabled(m_backend && m_backend->isRunning() && (m_rxActive || m_audioConnected));
    m_rxToggle->setProperty("role", m_rxActive ? "danger" : "primary");
    m_rxToggle->style()->unpolish(m_rxToggle);
    m_rxToggle->style()->polish(m_rxToggle);
}

void MainWindow::updateTXToggle() {
    if (!m_txToggle) return;
    m_txToggle->setText(m_txPreparing
        ? m_localeDictionary.value(QStringLiteral("tx.preparing")).toString(uiText(QStringLiteral("正在准备…")))
        : (m_txActive
            ? m_localeDictionary.value(QStringLiteral("tx.stop")).toString(uiText(QStringLiteral("停止发射")))
            : m_localeDictionary.value(QStringLiteral("tx.start")).toString(uiText(QStringLiteral("开始发射")))));
    m_txToggle->setProperty("role", m_txActive ? "danger" : "primary");
    m_txToggle->setEnabled(m_backend && m_backend->isRunning() && (m_txActive || (m_audioConnected && !m_txToneActive && !m_txPreparing)));
    m_txToggle->style()->unpolish(m_txToggle);
    m_txToggle->style()->polish(m_txToggle);
    if (m_txToneToggle) {
        m_txToneToggle->setText(m_txToneActive
            ? m_localeDictionary.value(QStringLiteral("tx.testToneActive")).toString(uiText(QStringLiteral("停止测试音")))
            : m_localeDictionary.value(QStringLiteral("tx.testTone")).toString(uiText(QStringLiteral("1500 Hz 测试音"))));
        m_txToneToggle->setEnabled(m_txToneActive || (!m_txActive && !m_txPreparing && m_audioConnected && m_backend && m_backend->isRunning()));
    }
}

void MainWindow::updateRepeatControls() {
    const bool repeatChecked = m_repeat && m_repeat->isChecked();
    const bool canToggleRepeat = !m_txPreparing && (!m_txActive || m_txCountdownActive);
    if (m_repeat) m_repeat->setEnabled(canToggleRepeat);
    if (m_repeatInterval) {
        // The spin box is a display-only countdown while repeat is enabled.
        m_repeatInterval->setEnabled(!repeatChecked && !m_txCountdownActive);
        if (!repeatChecked && !m_txCountdownActive)
            m_repeatInterval->setValue(m_repeatIntervalSeconds);
    }
}

void MainWindow::toggleRX() { if (m_rxActive) stopRX(); else startRX(); }
void MainWindow::toggleTX() {
    if (m_txActive) {
        m_backend->call(QStringLiteral("StopTX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
            if (!ok) showError(error);
        });
    } else {
        startTX();
    }
}

void MainWindow::applyLanguage(const QString &locale) {
    if (!m_backend || !m_backend->isRunning()) return;
    m_settings.insert(QStringLiteral("language"), locale);
    saveSettings(m_settings);
    QString error;
    const QJsonObject dictionary = m_backend->callSync(QStringLiteral("GetLocaleDictionary"), QJsonArray{locale}, &error, 5000).toObject();
    m_localeDictionary = error.isEmpty() ? dictionary : QJsonObject{};
    setUiLocale(locale);
    translateWidgetTree(this);
    if (!error.isEmpty()) statusBar()->showMessage(uiText(QStringLiteral("无法加载语言词典，使用内置翻译。")), 5000);
    const QString workbench = valueOr(dictionary, QStringLiteral("nav.workbench"), locale == QStringLiteral("en") ? QStringLiteral("Workbench") : locale.startsWith(QStringLiteral("ja")) ? uiText(QStringLiteral("ワークベンチ")) : uiText(QStringLiteral("工作台")));
    const QString album = valueOr(dictionary, QStringLiteral("nav.album"), locale == QStringLiteral("en") ? QStringLiteral("Album") : locale.startsWith(QStringLiteral("ja")) ? uiText(QStringLiteral("アルバム")) : uiText(QStringLiteral("相册")));
    if (m_workbenchNav) m_workbenchNav->setText(workbench);
    if (m_albumNav) m_albumNav->setText(album);
    updateRXToggle();
    updateTXToggle();
    statusBar()->showMessage(uiText(QStringLiteral("语言偏好已保存：%1")).arg(locale), 3000);
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
        appendLog(uiText(QStringLiteral("读取相册失败：")) + error);
        statusBar()->showMessage(uiText(QStringLiteral("读取相册失败：%1")).arg(error), 6000);
        return;
    }
    m_album = result.toArray();
    QList<QJsonObject> orderedAlbum;
    orderedAlbum.reserve(m_album.size());
    for (const QJsonValue &entry : m_album) orderedAlbum.push_back(entry.toObject());
    std::stable_sort(orderedAlbum.begin(), orderedAlbum.end(), [](const QJsonObject &a, const QJsonObject &b) {
        return a.value(QStringLiteral("createdAt")).toString() > b.value(QStringLiteral("createdAt")).toString();
    });

    while (QLayoutItem *item = m_albumCardsLayout->takeAt(0)) {
        if (QWidget *widget = item->widget()) widget->deleteLater();
        delete item;
    }

    const int filter = m_albumFilter ? m_albumFilter->currentIndex() : 0;
    const int availableWidth = m_albumCardsHost->width() > 0 ? m_albumCardsHost->width() : 1480;
    const int columns = qBound(1, availableWidth / 210, 7);
    int visibleRow = 0;
    int visibleColumn = 0;

    for (const QJsonObject &record : orderedAlbum) {
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
        thumb->setText(uiText(QStringLiteral("正在读取图像…")));
        cardLayout->addWidget(thumb);
        QPointer<QPushButton> safeThumb(thumb);
        m_backend->call(QStringLiteral("GetAlbumImageData"), QJsonArray{id}, [this, safeThumb](bool ok, const QJsonValue &value, const QString &) {
            if (!safeThumb || !ok) return;
            const QString dataUrl = value.toString();
            const QImage thumbnail = imageFromDataUrl(dataUrl);
            if (thumbnail.isNull()) {
                safeThumb->setText(uiText(QStringLiteral("图像预览不可用")));
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
                if (previewImage.isNull()) { statusBar()->showMessage(uiText(QStringLiteral("无法解码相册图像")), 6000); return; }
                QDialog preview(this);
                const bool animatedGIF = dataUrl.startsWith(QStringLiteral("data:image/gif"), Qt::CaseInsensitive);
                preview.setWindowTitle(animatedGIF ? uiText(QStringLiteral("GIF 动图预览")) : uiText(QStringLiteral("图像预览")));
                auto *layout = new QVBoxLayout(&preview);
                auto *label = new QLabel(&preview);
                label->setAlignment(Qt::AlignCenter);
                layout->addWidget(label);

                if (animatedGIF) {
                    auto *buffer = new QBuffer(&preview);
                    buffer->setData(imageDataUrlBytes(dataUrl));
                    if (buffer->open(QIODevice::ReadOnly)) {
                        auto *movie = new QMovie(buffer, QByteArrayLiteral("gif"), &preview);
                        movie->setScaledSize(previewImage.size().scaled(QSize(1200, 850), Qt::KeepAspectRatio));
                        if (movie->jumpToFrame(0) && movie->isValid()) {
                            label->setMovie(movie);
                            movie->start();
                        } else {
                            label->setPixmap(QPixmap::fromImage(previewImage).scaled(1200, 850, Qt::KeepAspectRatio, Qt::SmoothTransformation));
                        }
                    } else {
                        label->setPixmap(QPixmap::fromImage(previewImage).scaled(1200, 850, Qt::KeepAspectRatio, Qt::SmoothTransformation));
                    }
                } else {
                    label->setPixmap(QPixmap::fromImage(previewImage).scaled(1200, 850, Qt::KeepAspectRatio, Qt::SmoothTransformation));
                }
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
        const QString dimensions = QStringLiteral("%1×%2").arg(record.value(QStringLiteral("imageWidth")).toInt()).arg(record.value(QStringLiteral("imageHeight")).toInt());
        auto *metadata = new QLabel(QStringLiteral("%1\n%2 · ID %3 · %4 · %5 · %6")
            .arg(timeText, callsign)
            .arg(record.value(QStringLiteral("imageId")).toInt())
            .arg(dimensions, packets, record.value(QStringLiteral("status")).toString()), card);
        metadata->setWordWrap(true);
        metadata->setStyleSheet(QStringLiteral("font-size:8pt; color:#74808a;"));
        cardLayout->addWidget(metadata);
        if (quality != QStringLiteral("-")) {
            auto *qualityLabel = new QLabel(uiText(QStringLiteral("质量：%1")).arg(quality), card);
            qualityLabel->setStyleSheet(QStringLiteral("font-size:8pt; color:#89939b;"));
            cardLayout->addWidget(qualityLabel);
        }

        auto *actions = new QHBoxLayout();
        auto *exportButton = new QPushButton(uiText(QStringLiteral("导出文件")), card);
        auto *deleteButton = new QPushButton(uiText(QStringLiteral("删除记录")), card);
        deleteButton->setProperty("role", "danger");
        actions->addWidget(exportButton);
        actions->addStretch();
        actions->addWidget(deleteButton);
        cardLayout->addLayout(actions);
        connect(exportButton, &QPushButton::clicked, this, [this, id]() {
            const QString dir = QFileDialog::getExistingDirectory(this, uiText(QStringLiteral("导出图像")), QStandardPaths::writableLocation(QStandardPaths::PicturesLocation));
            if (dir.isEmpty()) return;
            m_backend->call(QStringLiteral("ExportAlbumRecordToDirectory"), QJsonArray{id, dir}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (ok) statusBar()->showMessage(uiText(QStringLiteral("相册图像已导出")), 4000);
                else showError(error);
            });
        });
        connect(deleteButton, &QPushButton::clicked, this, [this, id]() {
            if (QMessageBox::question(this, uiText(QStringLiteral("删除图像记录")), uiText(QStringLiteral("确定删除所选相册记录吗？"))) != QMessageBox::Yes) return;
            m_backend->call(QStringLiteral("DeleteAlbumRecord"), QJsonArray{id}, [this](bool ok, const QJsonValue &, const QString &error) {
                if (ok) refreshAlbum(); else showError(error);
            });
        });

        m_albumCardsLayout->addWidget(card, visibleRow, visibleColumn);
        ++visibleColumn;
        if (visibleColumn >= columns) { visibleColumn = 0; ++visibleRow; }
    }

    if (visibleRow == 0 && visibleColumn == 0) {
        auto *empty = new QLabel(uiText(QStringLiteral("相册暂无图像。")), m_albumCardsHost);
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
        if (payload.value(QStringLiteral("hasPreview")).toBool() || payload.value(QStringLiteral("gifEnd")).toBool()) {
            refreshLatestRXPreview();
        }
        m_templateImageCache.remove(QStringLiteral("last-rx"));
        refreshTemplateCanvas();
        const QString profile = payload.value(QStringLiteral("profile")).toString();
        const QString callsign = payload.value(QStringLiteral("callsign")).toString();
        if (!callsign.trimmed().isEmpty() && !m_hisCallManuallyEdited) {
            const bool cqTemplate = selectedTemplate().value(QStringLiteral("name")).toString().trimmed().compare(QStringLiteral("CQ"), Qt::CaseInsensitive) == 0;
            if (!(m_txProfile && m_txProfile->currentData().toString() == QStringLiteral("ssdv-ax25") && cqTemplate)) {
                m_hisCall->setText(callsign.trimmed().toUpper());
                refreshTemplateCanvas();
            }
        }
        const int received = payload.value(QStringLiteral("packetsReceived")).toInt();
        const int expected = payload.value(QStringLiteral("packetsExpected")).toInt();
        const int imageId = payload.value(QStringLiteral("imageId")).toInt();
        const double coverage = payload.value(QStringLiteral("coverage")).toDouble() * 100.0;
        const bool complete = payload.value(QStringLiteral("complete")).toBool();
        const bool hasSSID = payload.value(QStringLiteral("hasAX25SSID")).toBool();
        const QString ssid = hasSSID ? QStringLiteral("   SSID  %1").arg(payload.value(QStringLiteral("ax25SourceSSID")).toInt()) : QString();
        QString shownProfile = profile;
        if (m_rxProfile && m_rxProfile->currentData().toString() == QStringLiteral("auto")) {
            for (const QJsonValue &v : m_profiles) {
                const QJsonObject item = v.toObject();
                if (item.value(QStringLiteral("id")).toString() == profile) {
                    shownProfile = uiText(QStringLiteral("自动识别 · %1")).arg(valueOr(item, QStringLiteral("name"), profile));
                    break;
                }
            }
        }
        const QString codec = payload.value(QStringLiteral("codec")).toString();
        const bool gifMode = codec == QStringLiteral("SSDV-GIF") || payload.value(QStringLiteral("gifMode")).toBool();
        const int gifFrame = payload.value(QStringLiteral("gifFrame")).toInt();
        const bool gifFrameComplete = payload.value(QStringLiteral("gifFrameComplete")).toBool();
        const bool gifEnd = payload.value(QStringLiteral("gifEnd")).toBool();
        const bool gifSaved = payload.value(QStringLiteral("gifSaved")).toBool();
        const bool gifFrameAbandoned = payload.value(QStringLiteral("gifFrameAbandoned")).toBool();
        const bool gifAwaitingKeyframe = payload.value(QStringLiteral("gifAwaitingKeyframe")).toBool();
        QString gifReceiveState = gifFrameComplete ? uiText(QStringLiteral("帧完成")) : uiText(QStringLiteral("解码中"));
        if (gifFrameAbandoned) {
            gifReceiveState = uiText(QStringLiteral("帧不完整，继续重同步"));
        } else if (gifAwaitingKeyframe) {
            gifReceiveState = uiText(QStringLiteral("等待关键帧，增量帧仍继续接收"));
        }
        const QString receiveMode = gifMode
            ? uiText(QStringLiteral("GIF 第 %1 帧 · %2")).arg(gifFrame).arg(gifReceiveState)
            : (complete ? uiText(QStringLiteral("完成")) : uiText(QStringLiteral("接收中")));
        m_rxStatus->setText(QStringLiteral("%1 · %2 · %3% · %4")
            .arg(shownProfile, callsign).arg(coverage, 0, 'f', 1).arg(receiveMode));
        m_rxMeta->setText(QStringLiteral("CALL  %1%2   IMAGE ID  %3   PACKETS  %4 / %5   MODE  %6")
            .arg(callsign, ssid).arg(imageId).arg(received).arg(expected).arg(shownProfile));
        if (gifMode && !payload.value(QStringLiteral("gifSaveError")).toString().isEmpty()) {
            statusBar()->showMessage(uiText(QStringLiteral("GIF 已解码，但相册保存失败：%1")).arg(payload.value(QStringLiteral("gifSaveError")).toString()), 8000);
        } else if (gifMode && gifEnd && gifSaved && payload.value(QStringLiteral("gifStreamIncomplete")).toBool()) {
            statusBar()->showMessage(uiText(QStringLiteral("GIF 接收中断，已保存可解码的部分动画（%1 帧）")).arg(gifFrame), 6000);
        } else if (gifMode && gifEnd && gifSaved) {
            statusBar()->showMessage(uiText(QStringLiteral("GIF 动图接收完成并已保存到相册（%1 帧）")).arg(gifFrame), 5000);
        } else if (gifMode && gifSaved) {
            statusBar()->showMessage(uiText(QStringLiteral("GIF 部分接收内容已自动保存到相册")), 3500);
        }
        if (gifMode ? gifSaved || gifEnd : complete) refreshAlbum();
    } else if(name==QStringLiteral("rx:diagnostics")){
        const auto p=payloadValue.toObject();
        const auto manager=p.value(QStringLiteral("manager")).toObject();
        const auto decoder=p.value(QStringLiteral("decoder")).toObject();
        QString modemState=QStringLiteral("N/A");
        QString profile=decoder.value(QStringLiteral("profile")).toString();
        bool locked=decoder.value(QStringLiteral("maryLocked")).toBool();
        int buffered=decoder.value(QStringLiteral("maryBufferedSamples")).toInt();
        quint64 reacquires=decoder.value(QStringLiteral("maryReacquireCount")).toInteger();
        QString fecMode=decoder.value(QStringLiteral("fecMode")).toString();
        qint64 fecOK=decoder.value(QStringLiteral("fecChunksOk")).toInteger();
        qint64 fecBad=decoder.value(QStringLiteral("fecChunksBad")).toInteger();
        QString fecError=decoder.value(QStringLiteral("lastFecError")).toString();
        const QString autoLocked=decoder.value(QStringLiteral("lockedProfile")).toString();
        if(!autoLocked.isEmpty()) profile=autoLocked;
        const auto candidates=decoder.value(QStringLiteral("maryCandidates")).toArray();
        for(const auto &entry:candidates){
            const auto candidate=entry.toObject();
            const QString candidateProfile=candidate.value(QStringLiteral("profile")).toString();
            if((!profile.isEmpty()&&candidateProfile==profile)||candidate.value(QStringLiteral("maryLocked")).toBool()){
                locked=candidate.value(QStringLiteral("maryLocked")).toBool();
                buffered=candidate.value(QStringLiteral("maryBufferedSamples")).toInt();
                reacquires=candidate.value(QStringLiteral("maryReacquireCount")).toInteger();
                fecMode=candidate.value(QStringLiteral("fecMode")).toString();
                fecOK=candidate.value(QStringLiteral("fecChunksOk")).toInteger();
                fecBad=candidate.value(QStringLiteral("fecChunksBad")).toInteger();
                fecError=candidate.value(QStringLiteral("lastFecError")).toString();
                break;
            }
        }
        if(!profile.isEmpty() && profile.contains(QStringLiteral("1600")))
            modemState=locked?QStringLiteral("LOCK"):QStringLiteral("SEARCH");
        const QJsonArray streams=manager.value(QStringLiteral("gifStreams")).toArray();
        QString gifState=QStringLiteral("GIF --");
        QString gifDetails;
        if(!streams.isEmpty()){
            const auto gif=streams.first().toObject();
            gifState=QStringLiteral("GIF F%1 %2/%3 MCU %4/%5%6")
                .arg(gif.value(QStringLiteral("frameIndex")).toInt())
                .arg(gif.value(QStringLiteral("framePacketsReceived")).toInt())
                .arg(gif.value(QStringLiteral("framePacketsExpected")).toInt())
                .arg(gif.value(QStringLiteral("decodedMcus")).toInt())
                .arg(gif.value(QStringLiteral("expectedMcus")).toInt())
                .arg(gif.value(QStringLiteral("awaitingKeyframe")).toBool()?QStringLiteral(" KEY"):QString());
            gifDetails=QStringLiteral("GIF callsign=%1 frame=%2 lastPacket=%3ms lastDecode=%4ms attempts=%5 error=%6")
                .arg(gif.value(QStringLiteral("callsign")).toString())
                .arg(gif.value(QStringLiteral("frameId")).toInt())
                .arg(gif.value(QStringLiteral("lastPacketAgeMs")).toInteger())
                .arg(gif.value(QStringLiteral("lastDecodeAgeMs")).toInteger())
                .arg(gif.value(QStringLiteral("decodeAttempts")).toInteger())
                .arg(gif.value(QStringLiteral("lastDecodeError")).toString());
        }
        const qint64 queueDrops=p.value(QStringLiteral("queueDrops")).toInteger()+manager.value(QStringLiteral("queueDropped")).toInteger()+p.value(QStringLiteral("managerSubmitDrops")).toInteger();
        m_rxDiagnostics->setText(QStringLiteral("AUD %1 blk · DEC %2 blk / %3 pkt · Q %4/%5 · DROP %6 · %7 %8 · FEC %9 %10/%11 · %12")
            .arg(p.value(QStringLiteral("audioBlocks")).toInteger())
            .arg(p.value(QStringLiteral("decoderBlocks")).toInteger())
            .arg(p.value(QStringLiteral("decoderPackets")).toInteger())
            .arg(p.value(QStringLiteral("queueDepth")).toInt()).arg(p.value(QStringLiteral("queueCapacity")).toInt())
            .arg(queueDrops).arg(profile).arg(modemState).arg(fecMode).arg(fecOK).arg(fecBad).arg(gifState));
        const QString tip=QStringLiteral("Input samples=%1; input discontinuities=%2; decoder resets=%3; decoder errors=%4; panics=%5; RX manager=%6/%7 dropped=%8 processed=%9; MARY buffered=%10 samples, reacquires=%11; %12; FEC error=%13; last error=%14")
            .arg(p.value(QStringLiteral("audioSamples")).toInteger())
            .arg(p.value(QStringLiteral("inputDiscontinuities")).toInteger())
            .arg(p.value(QStringLiteral("decoderResets")).toInteger())
            .arg(p.value(QStringLiteral("decoderErrors")).toInteger())
            .arg(p.value(QStringLiteral("decoderPanics")).toInteger())
            .arg(manager.value(QStringLiteral("queueDepth")).toInt()).arg(manager.value(QStringLiteral("queueCapacity")).toInt())
            .arg(manager.value(QStringLiteral("queueDropped")).toInteger())
            .arg(manager.value(QStringLiteral("processedPackets")).toInteger())
            .arg(buffered).arg(reacquires).arg(gifDetails).arg(fecError).arg(p.value(QStringLiteral("lastError")).toString());
        m_rxDiagnostics->setToolTip(tip);
    } else if(name==QStringLiteral("rx:decoder-error")){
        const auto p=payloadValue.toObject();
        const QString error=p.value(QStringLiteral("error")).toString();
        statusBar()->showMessage(uiText(QStringLiteral("RX 解码器发生异常，已重置并继续监听：%1")).arg(error),10000);
        appendLog(uiText(QStringLiteral("RX decoder recovered from error: %1")).arg(error));
    } else if(name==QStringLiteral("rx:level")){
        const auto p=payloadValue.toObject();m_rxStatus->setToolTip(QStringLiteral("RMS %1 dBFS · Peak %2").arg(p.value(QStringLiteral("rmsDbfs")).toDouble(),0,'f',1).arg(p.value(QStringLiteral("peak")).toDouble(),0,'f',3));
        if (m_rxSnrLabel) {
            if (p.value(QStringLiteral("snrValid")).toBool()) {
                const QString method = p.value(QStringLiteral("snrReason")).toString();
                if (method == QStringLiteral("mary-evm")) {
                    m_rxSnrLabel->setText(QStringLiteral("EVM-SNR %1 dB").arg(p.value(QStringLiteral("snrDb")).toDouble(), 0, 'f', 1));
                    m_rxSnrLabel->setToolTip(uiText(QStringLiteral("M-ary 后解调 EVM-SNR 估算值 · 从已同步星座符号测量 · 参考带宽为符号判决孔径，不可直接与全带宽 PCM SNR、RF SNR 或 Eb/N0 比较")));
                } else {
                    m_rxSnrLabel->setText(uiText(QStringLiteral("SNR %1 dB")).arg(p.value(QStringLiteral("snrDb")).toDouble(), 0, 'f', 1));
                    m_rxSnrLabel->setToolTip(uiText(QStringLiteral("AFSK1200 音频 SNR 估算 · 双音频谱特征 + 解调同步 + 最近 1.2 秒内 CRC 校验通过的数据包 · 未经射频校准 · 噪声底 %1 dBFS/bin")).arg(p.value(QStringLiteral("noiseFloorDbfs")).toDouble(), 0, 'f', 1));
                }
            } else {
                m_rxSnrLabel->setText(uiText(QStringLiteral("SNR -- dB")));
                const QString reason = p.value(QStringLiteral("snrReason")).toString();
                if (reason == QStringLiteral("mary-awaiting-sync")) {
                    m_rxSnrLabel->setToolTip(uiText(QStringLiteral("M-ary EVM-SNR 等待载波/定时同步及至少 48 个有效符号")));
                } else if (reason == QStringLiteral("auto-awaiting-sync")) {
                    m_rxSnrLabel->setToolTip(uiText(QStringLiteral("Auto RX 正在等待已同步的解调候选；M-ary 候选锁定后会显示后解调 EVM-SNR")));
                } else if (reason == QStringLiteral("wideband-estimator-unavailable")) {
                    m_rxSnrLabel->setToolTip(uiText(QStringLiteral("当前模式暂不提供可靠的解调域 SNR 估算，因此不显示频谱推算值。")));
                } else {
                    m_rxSnrLabel->setToolTip(uiText(QStringLiteral("AFSK1200 音频 SNR 正在等待空闲噪声基线、双音频谱特征、解调同步和有效 CRC 数据包")));
                }
            }
        }
    } else if(name==QStringLiteral("rx:spectrum")||name==QStringLiteral("tx:spectrum")){
        const auto bins=payloadValue.toArray(); if(!bins.isEmpty()){
            SpectrumWaterfallWidget *target = name.startsWith(QStringLiteral("rx")) ? m_rxSpectrum : m_txSpectrum;
            QLabel *status = name.startsWith(QStringLiteral("rx")) ? m_rxSpectrumStatus : m_txSpectrumStatus;
            target->pushBins(bins);
            double peak=-120.0;int peakHz=0;for(const auto &b:bins){const auto o=b.toObject();const double db=o.value(QStringLiteral("magnitudeDbfs")).toDouble(-120);if(db>peak){peak=db;peakHz=o.value(QStringLiteral("frequencyHz")).toInt();}}
            status->setText(uiText(QStringLiteral("%1 bins · 峰值 %2 dBFS @ %3 Hz")).arg(bins.size()).arg(peak,0,'f',1).arg(peakHz));
        }
    } else if(name==QStringLiteral("tx:progress")){
        m_txProgress->setValue(static_cast<int>(payloadValue.toDouble()*1000.0));
    } else if(name==QStringLiteral("tx:metrics")){
        const auto p = payloadValue.toObject();
        const QString estimated = formatTXDuration(p.value(QStringLiteral("estimatedSeconds")).toInteger());
        const QString elapsed = formatTXDuration(p.value(QStringLiteral("elapsedSeconds")).toInteger());
        const QString remaining = formatTXDuration(p.value(QStringLiteral("remainingSeconds")).toInteger());
        const qint64 sent = qMax<qint64>(0, p.value(QStringLiteral("sentDataFrames")).toInteger());
        const qint64 total = qMax<qint64>(0, p.value(QStringLiteral("totalDataFrames")).toInteger());
        m_txMetrics->setText(uiText(QStringLiteral("预计 %1 · 已发射 %2 · 剩余 %3 · 数据帧 %4 / %5"))
            .arg(estimated, elapsed, remaining).arg(sent).arg(total));
    } else if(name==QStringLiteral("tx:state")){
        m_txActive = payloadValue.toObject().value(QStringLiteral("active")).toBool();
        if (m_txSpectrum) m_txSpectrum->setStreaming(m_txActive);
        if (!m_txActive) m_txCountdownActive = false;
        updateRepeatControls();
        updateTXToggle();
        if(!m_txActive){
            m_txProgress->setValue(0);
            m_txMetrics->setText(uiText(QStringLiteral("预计 %1 · 已发射 %2 · 剩余 %3 · 数据帧 %4 / %5"))
                .arg(QStringLiteral("00:00:00"), QStringLiteral("00:00:00"), QStringLiteral("00:00:00"), QStringLiteral("0"), QStringLiteral("0")));
            refreshAlbum();
        }
    } else if(name==QStringLiteral("tx:tone-state")){
        m_txToneActive = payloadValue.toObject().value(QStringLiteral("active")).toBool();
        updateTXToggle();
    } else if(name==QStringLiteral("tx:status")){
        const auto p=payloadValue.toObject();
        const QString phase=p.value(QStringLiteral("phase")).toString();
        if (phase == QStringLiteral("countdown") && m_repeat && m_repeat->isChecked()) {
            m_txCountdownActive = true;
            updateRepeatControls();
            const int remaining = qMax(0, p.value(QStringLiteral("remainingSeconds")).toInt());
            m_repeatInterval->setValue(remaining);
            statusBar()->showMessage(uiText(QStringLiteral("循环发射倒计时：%1 秒")).arg(remaining));
        } else if (phase == QStringLiteral("sending") || phase == QStringLiteral("completed")) {
            // Keep the display at zero during transmission. When a burst
            // finishes, TX waterfall enters idle-scroll mode and clears old
            // rows during the inter-cycle countdown.
            m_txCountdownActive = false;
            if (m_txSpectrum) m_txSpectrum->setStreaming(phase == QStringLiteral("sending"));
            updateRepeatControls();
        } else if (phase == QStringLiteral("cancelled")) {
            m_txCountdownActive = false;
            m_repeatInterval->setValue(m_repeatIntervalSeconds);
            updateRepeatControls();
        }
        appendLog(QStringLiteral("TX: ")+QJsonDocument(p).toJson(QJsonDocument::Compact));
    } else if(name.endsWith(QStringLiteral(":error"))||name==QStringLiteral("ptt:error")||name==QStringLiteral("audio:error")){
        showError(payloadValue.isString()?payloadValue.toString():QJsonDocument(payloadValue.toObject()).toJson(QJsonDocument::Compact));
    }
}

void MainWindow::startRX() {
    if (!m_audioConnected) { showError(uiText(QStringLiteral("音频后端异常"))); return; }
    if (m_rxPreview) m_rxPreview->clearSourceImage();
    if (m_rxMeta) m_rxMeta->setText(QStringLiteral("CALL  ---   IMAGE ID  --   PACKETS  0 / 0   MODE  ---"));
    if (m_rxDiagnostics) m_rxDiagnostics->setText(uiText(QStringLiteral("RX 解码诊断 · 等待音频")));
    if (m_rxSnrLabel) m_rxSnrLabel->setText(uiText(QStringLiteral("SNR -- dB")));
    m_settings.insert(QStringLiteral("rxProfile"), m_rxProfile->currentData().toString());
    QString saveError;
    m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &saveError, 15000);
    if (!saveError.isEmpty()) { showError(saveError); return; }
    m_backend->call(QStringLiteral("StartRX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
        if (ok) { m_rxActive = true; m_rxStatus->setText(uiText(QStringLiteral("RX 已启动"))); updateRXToggle(); appendLog(uiText(QStringLiteral("接收已启动"))); }
        else showError(error);
    });
}
void MainWindow::stopRX() {
    m_backend->call(QStringLiteral("StopRX"), {}, [this](bool ok, const QJsonValue &, const QString &error) {
        if (ok) { m_rxActive = false; m_rxStatus->setText(uiText(QStringLiteral("RX 已停止"))); if (m_rxSnrLabel) m_rxSnrLabel->setText(uiText(QStringLiteral("SNR -- dB"))); updateRXToggle(); appendLog(uiText(QStringLiteral("接收已停止"))); }
        else showError(error);
    });
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
            const bool gifImage = data.startsWith(QStringLiteral("data:image/gif"), Qt::CaseInsensitive);
            const qreal fit=qMin(size.width()/qreal(component.width()),size.height()/qreal(component.height()));const qreal factor=fit*qBound(0.01,c.value(QStringLiteral("scale")).toDouble(35)/100.0,1.0);const QSizeF target(component.width()*factor,component.height()*factor);QRectF rect(x,y,target.width(),target.height());
            if (!gifImage && c.value(QStringLiteral("outline")).toBool(true) && c.value(QStringLiteral("outlineWidth")).toDouble(2.0) > 0) {
                drawAlphaContourOutline(painter, component, rect,
                    QColor(valueOr(c, QStringLiteral("outlineColor"), QStringLiteral("#000000"))),
                    c.value(QStringLiteral("outlineWidth")).toDouble(2.0) * size.height() / 480.0);
            } else {
                painter.drawImage(rect, component);
            }
        }
    }
    painter.end();if(errorMessage)errorMessage->clear();return image;
}

QJsonObject MainWindow::buildRenderedTXPayload(const QImage &preview, QString *errorMessage) {
    QJsonObject rendered;
    if (preview.isNull()) {
        if (errorMessage) *errorMessage = uiText(QStringLiteral("无法生成发送图像"));
        return rendered;
    }
    QByteArray previewPNG;
    QBuffer previewBuffer(&previewPNG);
    if (!previewBuffer.open(QIODevice::WriteOnly) || !preview.save(&previewBuffer, "PNG")) {
        if (errorMessage) *errorMessage = uiText(QStringLiteral("无法将发送模板编码为 PNG"));
        return rendered;
    }
    rendered.insert(QStringLiteral("id"), QStringLiteral("qt-%1").arg(QDateTime::currentMSecsSinceEpoch()));
    rendered.insert(QStringLiteral("name"), valueOr(selectedTemplate(), QStringLiteral("name"), QStringLiteral("Qt Native Template")));
    rendered.insert(QStringLiteral("pngBase64"), QString::fromLatin1(previewPNG.toBase64()));

    struct TXLayer {
        QJsonObject component;
        QString type;
        QString dataUrl;
        QImage image;
        bool gif = false;
    };
    const QSize size = preview.size();
    const QJsonObject templateObject = selectedTemplate();
    const QJsonArray components = templateObject.value(QStringLiteral("components")).toArray();
    QVector<TXLayer> layers;
    layers.reserve(components.size());
    int gifCount = 0;
    int gifLayerIndex = -1;

    for (const QJsonValue &value : components) {
        const QJsonObject component = value.toObject();
        if (component.contains(QStringLiteral("visible")) && !component.value(QStringLiteral("visible")).toBool()) continue;
        const QString type = component.value(QStringLiteral("type")).toString();
        if (type != QStringLiteral("text") && type != QStringLiteral("image") && type != QStringLiteral("last-rx-image")) continue;
        TXLayer layer;
        layer.component = component;
        layer.type = type;
        if (type == QStringLiteral("image") || type == QStringLiteral("last-rx-image")) {
            QString loadError;
            layer.dataUrl = type == QStringLiteral("image")
                ? m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{component.value(QStringLiteral("assetId")).toString()}, &loadError, 10000).toString()
                : m_backend->callSync(QStringLiteral("GetLatestRXImageData"), {}, &loadError, 10000).toString();
            if (!loadError.isEmpty()) {
                if (errorMessage) *errorMessage = loadError;
                return {};
            }
            layer.image = imageFromDataUrl(layer.dataUrl);
            layer.gif = type == QStringLiteral("image") && layer.dataUrl.startsWith(QStringLiteral("data:image/gif"), Qt::CaseInsensitive);
            if (layer.gif) {
                ++gifCount;
                gifLayerIndex = layers.size();
                if (layer.image.isNull()) {
                    if (errorMessage) *errorMessage = uiText(QStringLiteral("无法解码模板中的 GIF 图像，请重新导入 GIF 文件"));
                    return {};
                }
            }
        }
        layers.push_back(std::move(layer));
    }

    if (gifCount == 0) {
        if (errorMessage) errorMessage->clear();
        return rendered;
    }
    if (gifCount > 1) {
        if (errorMessage) *errorMessage = uiText(QStringLiteral("当前每个模板最多只能发射一个可见 GIF 动图"));
        return {};
    }

    QImage base(size, QImage::Format_ARGB32_Premultiplied);
    base.fill(QColor(QStringLiteral("#17232d")));
    QImage overlay(size, QImage::Format_ARGB32_Premultiplied);
    overlay.fill(Qt::transparent);

    const QString backgroundId = valueOr(m_settings, QStringLiteral("templateBackgroundAssetId"), templateObject.value(QStringLiteral("backgroundAssetId")).toString());
    if (!backgroundId.isEmpty()) {
        QString backgroundError;
        const QString data = m_backend->callSync(QStringLiteral("GetGalleryImageData"), QJsonArray{backgroundId}, &backgroundError, 10000).toString();
        if (!backgroundError.isEmpty()) {
            if (errorMessage) *errorMessage = backgroundError;
            return {};
        }
        const QImage background = imageFromDataUrl(data);
        if (!background.isNull()) {
            QPainter painter(&base);
            painter.setRenderHint(QPainter::SmoothPixmapTransform);
            const qreal factor = qMax(size.width() / qreal(background.width()), size.height() / qreal(background.height()));
            const QSizeF scaled(background.width() * factor, background.height() * factor);
            painter.drawImage(QRectF((size.width() - scaled.width()) / 2.0, (size.height() - scaled.height()) / 2.0, scaled.width(), scaled.height()), background);
        }
    }

    auto drawLayer = [this, &size](QPainter &painter, const TXLayer &layer) {
        const QJsonObject &component = layer.component;
        const qreal x = size.width() * component.value(QStringLiteral("x")).toDouble() / 100.0;
        const qreal y = size.height() * component.value(QStringLiteral("y")).toDouble() / 100.0;
        if (layer.type == QStringLiteral("text")) {
            const qreal scale = size.height() / 480.0;
            const qreal fontSize = qMax(6.0, component.value(QStringLiteral("fontSize")).toDouble(24.0) * scale);
            QFont font(QStringLiteral("Segoe UI"));
            font.setPixelSize(qRound(fontSize));
            font.setBold(component.value(QStringLiteral("bold")).toBool());
            painter.setFont(font);
            const QString text = resolveTemplateText(component.value(QStringLiteral("text")).toString());
            const QColor fill(valueOr(component, QStringLiteral("color"), QStringLiteral("#ffffff")));
            const QColor outlineColor(valueOr(component, QStringLiteral("outlineColor"), QStringLiteral("#000000")));
            const qreal outlineWidth = component.value(QStringLiteral("outlineWidth")).toDouble(2.0) * scale;
            qreal baseline = y + fontSize;
            for (const QString &line : text.split(QLatin1Char('\n'))) {
                if (component.value(QStringLiteral("outline")).toBool(true) && outlineWidth > 0) {
                    QPainterPath path;
                    path.addText(QPointF(x, baseline), font, line);
                    painter.strokePath(path, QPen(outlineColor, outlineWidth * 2.0, Qt::SolidLine, Qt::RoundCap, Qt::RoundJoin));
                    painter.fillPath(path, fill);
                } else {
                    painter.setPen(fill);
                    painter.drawText(QPointF(x, baseline), line);
                }
                baseline += fontSize * 1.22;
            }
            return;
        }
        if (layer.image.isNull()) {
            if (layer.type == QStringLiteral("last-rx-image")) {
                const QRectF frame(x, y, size.width() * 0.35, size.height() * 0.35 * 0.75);
                painter.fillRect(frame, QColor(18, 30, 40, 220));
                painter.setPen(QColor(QStringLiteral("#d8e0e5")));
                painter.drawText(frame, Qt::AlignCenter, QStringLiteral("LAST RX"));
            }
            return;
        }
        const qreal fit = qMin(size.width() / qreal(layer.image.width()), size.height() / qreal(layer.image.height()));
        const qreal factor = fit * qBound(0.01, component.value(QStringLiteral("scale")).toDouble(35.0) / 100.0, 1.0);
        const QRectF rect(x, y, layer.image.width() * factor, layer.image.height() * factor);
        if (!layer.gif && component.value(QStringLiteral("outline")).toBool(true) && component.value(QStringLiteral("outlineWidth")).toDouble(2.0) > 0) {
            drawAlphaContourOutline(painter, layer.image, rect,
                QColor(valueOr(component, QStringLiteral("outlineColor"), QStringLiteral("#000000"))),
                component.value(QStringLiteral("outlineWidth")).toDouble(2.0) * size.height() / 480.0);
        } else if (!layer.gif) {
            painter.drawImage(rect, layer.image);
        }
    };

    QPainter basePainter(&base);
    basePainter.setRenderHint(QPainter::Antialiasing);
    basePainter.setRenderHint(QPainter::TextAntialiasing);
    basePainter.setRenderHint(QPainter::SmoothPixmapTransform);
    QPainter overlayPainter(&overlay);
    overlayPainter.setRenderHint(QPainter::Antialiasing);
    overlayPainter.setRenderHint(QPainter::TextAntialiasing);
    overlayPainter.setRenderHint(QPainter::SmoothPixmapTransform);

    bool drawAboveGIF = false;
    QRect gifRect;
    QByteArray gifBytes;
    for (int i = 0; i < layers.size(); ++i) {
        const TXLayer &layer = layers.at(i);
        if (i == gifLayerIndex) {
            const QJsonObject component = layer.component;
            const qreal x = size.width() * component.value(QStringLiteral("x")).toDouble() / 100.0;
            const qreal y = size.height() * component.value(QStringLiteral("y")).toDouble() / 100.0;
            const qreal fit = qMin(size.width() / qreal(layer.image.width()), size.height() / qreal(layer.image.height()));
            const qreal factor = fit * qBound(0.01, component.value(QStringLiteral("scale")).toDouble(35.0) / 100.0, 1.0);
            const QRectF destination(x, y, layer.image.width() * factor, layer.image.height() * factor);
            gifRect = QRect(qRound(destination.x()), qRound(destination.y()), qMax(1, qRound(destination.width())), qMax(1, qRound(destination.height())));
            gifBytes = imageDataUrlBytes(layer.dataUrl);
            drawAboveGIF = true;
            continue;
        }
        drawLayer(drawAboveGIF ? overlayPainter : basePainter, layer);
    }
    basePainter.end();
    overlayPainter.end();

    if (gifBytes.isEmpty() || gifRect.isEmpty()) {
        if (errorMessage) *errorMessage = uiText(QStringLiteral("无法提取模板 GIF 原始数据或显示区域"));
        return {};
    }
    QByteArray basePNG;
    QByteArray overlayPNG;
    QBuffer baseBuffer(&basePNG), overlayBuffer(&overlayPNG);
    if (!baseBuffer.open(QIODevice::WriteOnly) || !base.save(&baseBuffer, "PNG") ||
        !overlayBuffer.open(QIODevice::WriteOnly) || !overlay.save(&overlayBuffer, "PNG")) {
        if (errorMessage) *errorMessage = uiText(QStringLiteral("无法编码 GIF 模板的静态图层"));
        return {};
    }
    rendered.insert(QStringLiteral("gifBase64"), QString::fromLatin1(gifBytes.toBase64()));
    rendered.insert(QStringLiteral("overlayPNGBase64"), QString::fromLatin1(overlayPNG.toBase64()));
    rendered.insert(QStringLiteral("pngBase64"), QString::fromLatin1(basePNG.toBase64()));
    rendered.insert(QStringLiteral("gifRect"), QJsonObject{
        {QStringLiteral("x"), gifRect.x()}, {QStringLiteral("y"), gifRect.y()},
        {QStringLiteral("width"), gifRect.width()}, {QStringLiteral("height"), gifRect.height()}
    });
    if (errorMessage) errorMessage->clear();
    return rendered;
}

void MainWindow::startTX() {
    if (m_txPreparing || m_txActive || !m_backend || !m_backend->isRunning()) return;
    if (!m_audioConnected) { showError(uiText(QStringLiteral("音频后端异常"))); return; }
    m_txPreparing = true;
    m_txProgress->setValue(0);
    m_txMetrics->setText(uiText(QStringLiteral("预计 %1 · 已发射 %2 · 剩余 %3 · 数据帧 %4 / %5"))
        .arg(QStringLiteral("00:00:00"), QStringLiteral("00:00:00"), QStringLiteral("00:00:00"), QStringLiteral("0"), QStringLiteral("0")));
    updateRepeatControls();
    updateTXToggle();

    m_settings.insert(QStringLiteral("callsign"), m_callsign->text().trimmed());
    m_settings.insert(QStringLiteral("locator"), m_locator->text().trimmed());
    m_settings.insert(QStringLiteral("txProfile"), m_txProfile->currentData().toString());
    m_settings.insert(QStringLiteral("quality"), selectedQuality());
    if (m_ax25SSID) m_settings.insert(QStringLiteral("ax25Ssid"), m_ax25SSID->value());

    QString ax25To = QStringLiteral("CQ");
    if (m_txProfile->currentData().toString() == QStringLiteral("ssdv-ax25")) {
        clearHisCallForCQTemplate();
        const QString templateName = selectedTemplate().value(QStringLiteral("name")).toString().trimmed();
        const bool cqTemplate = templateName.compare(QStringLiteral("CQ"), Qt::CaseInsensitive) == 0;
        const QString hisCall = m_hisCall ? m_hisCall->text().trimmed().toUpper() : QString();
        if (!cqTemplate && !hisCall.isEmpty()) ax25To = hisCall;
    }

    QString saveError;
    m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{m_settings}, &saveError, 15000);
    if (!saveError.isEmpty()) {
        m_txPreparing = false;
        updateTXToggle();
        showError(saveError);
        return;
    }

    QString renderError;
    QImage image = renderTemplate(&renderError);
    if (image.isNull()) {
        m_txPreparing = false;
        updateTXToggle();
        showError(renderError.isEmpty() ? uiText(QStringLiteral("无法生成发送图像")) : renderError);
        return;
    }
    m_txPreview->setSourceImage(image);
    QString payloadError;
    QJsonObject rendered = buildRenderedTXPayload(image, &payloadError);
    if (rendered.isEmpty()) {
        m_txPreparing = false;
        updateRepeatControls();
        updateTXToggle();
        showError(payloadError.isEmpty() ? uiText(QStringLiteral("无法准备发送模板")) : payloadError);
        return;
    }
    const int repeatInterval = m_repeat->isChecked() ? m_repeatIntervalSeconds : m_repeatInterval->value();
    const QJsonArray args{rendered, m_repeat->isChecked(), repeatInterval, ax25To};
    m_backend->call(QStringLiteral("StartTXRendered"), args, [this](bool ok, const QJsonValue &, const QString &error) {
        m_txPreparing = false;
        updateRepeatControls();
        if (ok) {
            m_txActive = true;
            updateRepeatControls();
            updateTXToggle();
            m_txProgress->setValue(0);
            appendLog(uiText(QStringLiteral("已提交图像发射任务")));
        } else {
            updateTXToggle();
            showError(error);
        }
    });
}

void MainWindow::saveSettings(const QJsonObject &settings) {
    m_backend->call(QStringLiteral("SaveSettings"),QJsonArray{settings},[this](bool ok,const QJsonValue&,const QString &error){if(!ok)appendLog(uiText(QStringLiteral("保存设置失败："))+error);});
}

void MainWindow::openSettings() {
    QString error;
    QJsonObject settings = m_backend->callSync(QStringLiteral("GetSettings"), {}, &error).toObject();
    if (!error.isEmpty()) { showError(error); return; }

    QDialog dialog(this);
    dialog.setWindowTitle(uiText(QStringLiteral("设备与电台设置")));
    dialog.resize(620, 760);
    auto *layout = new QVBoxLayout(&dialog);
    auto *settingsTabs = new QTabWidget(&dialog);
    auto *generalPage = new QWidget(settingsTabs);
    auto *generalLayout = new QVBoxLayout(generalPage);
    auto *form = new QFormLayout();

    auto *callsign = new QLineEdit(settings.value(QStringLiteral("callsign")).toString(), &dialog);
    auto *locator = new QLineEdit(settings.value(QStringLiteral("locator")).toString(), &dialog);
    auto *language = makeCombo(&dialog, {uiText(QStringLiteral("简体中文")), QStringLiteral("English"), uiText(QStringLiteral("日本語"))});
    const QString locale = settings.value(QStringLiteral("language")).toString();
    language->setCurrentIndex(locale == QStringLiteral("en") ? 1 : locale.startsWith(QStringLiteral("ja")) ? 2 : 0);

    auto *sample = makeCombo(&dialog, {QStringLiteral("19200"), QStringLiteral("44100"), QStringLiteral("48000"), QStringLiteral("96000")});
    const int rateIndex = sample->findText(QString::number(settings.value(QStringLiteral("audioSampleRate")).toInt(48000)));
    if (rateIndex >= 0) sample->setCurrentIndex(rateIndex);
    auto *maryFEC = makeCombo(&dialog, {uiText(QStringLiteral("速度优先（均衡 FEC）")), uiText(QStringLiteral("弱信号（高冗余 FEC）"))});
    maryFEC->setCurrentIndex(settings.value(QStringLiteral("maryFecProfile")).toString() == QStringLiteral("robust") ? 1 : 0);
    maryFEC->setToolTip(uiText(QStringLiteral("均衡：QPSK 5/6、8PSK 3/4、16QAM 2/3，并按调制方式添加包级纠删冗余；高冗余：全部采用 1/2 卷积码和 8+2 包级恢复。高冗余会增加发射时间。")));

    auto *rxDevice = new QComboBox(&dialog);
    auto *txDevice = new QComboBox(&dialog);
    rxDevice->addItem(uiText(QStringLiteral("系统默认")), QString());
    txDevice->addItem(uiText(QStringLiteral("系统默认")), QString());
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
        appendLog(uiText(QStringLiteral("音频设备列表不可用：")) + error);
    }
    int index = rxDevice->findData(settings.value(QStringLiteral("rxInputDevice")).toString());
    if (index >= 0) rxDevice->setCurrentIndex(index);
    index = txDevice->findData(settings.value(QStringLiteral("txOutputDevice")).toString());
    if (index >= 0) txDevice->setCurrentIndex(index);

    const QJsonObject ptt = settings.value(QStringLiteral("ptt")).toObject();
    auto *pttMode = makeCombo(&dialog, {QStringLiteral("VOX"), QStringLiteral("Hamlib Net Rigctl"), uiText(QStringLiteral("串口 RTS")), uiText(QStringLiteral("串口 DTR"))});
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
        if (!portsError.isEmpty()) { appendLog(uiText(QStringLiteral("串口列表不可用：")) + portsError); return; }
        const QString current = serial->currentText();
        serial->clear();
        for (const auto &value : portsValue.toArray()) serial->addItem(value.toString());
        serial->setCurrentText(current);
    };
    loadSerialPorts();
    serial->setCurrentText(ptt.value(QStringLiteral("serialPort")).toString());
    auto *refreshPorts = new QPushButton(uiText(QStringLiteral("刷新串口")), &dialog);
    auto *serialRow = new QWidget(&dialog);
    auto *serialRowLayout = new QHBoxLayout(serialRow);
    serialRowLayout->setContentsMargins(0, 0, 0, 0);
    serialRowLayout->addWidget(serial, 1);
    serialRowLayout->addWidget(refreshPorts);
    connect(refreshPorts, &QPushButton::clicked, &dialog, loadSerialPorts);

    auto *activeHigh = new QCheckBox(uiText(QStringLiteral("PTT 高电平有效")), &dialog);
    activeHigh->setChecked(ptt.value(QStringLiteral("activeHigh")).toBool(true));
    auto *lead = new QSpinBox(&dialog); lead->setRange(0, 10000); lead->setSuffix(QStringLiteral(" ms")); lead->setValue(settings.value(QStringLiteral("pttLeadMs")).toInt(300));
    auto *tail = new QSpinBox(&dialog); tail->setRange(0, 10000); tail->setSuffix(QStringLiteral(" ms")); tail->setValue(settings.value(QStringLiteral("pttTailMs")).toInt(250));

    form->addRow(uiText(QStringLiteral("本站呼号")), callsign);
    form->addRow(uiText(QStringLiteral("本站网格")), locator);
    form->addRow(uiText(QStringLiteral("界面语言")), language);
    form->addRow(uiText(QStringLiteral("音频采样率")), sample);
    form->addRow(uiText(QStringLiteral("M-ary FEC 策略")), maryFEC);
    form->addRow(uiText(QStringLiteral("RX 音频输入")), rxDevice);
    form->addRow(uiText(QStringLiteral("TX 音频输出")), txDevice);
    form->addRow(uiText(QStringLiteral("PTT 控制方式")), pttMode);
    form->addRow(uiText(QStringLiteral("rigctld 地址")), rigctl);
    form->addRow(uiText(QStringLiteral("串口设备")), serialRow);
    form->addRow(QString(), activeHigh);
    form->addRow(uiText(QStringLiteral("PTT 发射前延时")), lead);
    form->addRow(uiText(QStringLiteral("PTT 发射后延时")), tail);
    auto *voxHint = new QLabel(uiText(QStringLiteral("VOX 由电台检测音频自动发射，不需要软件 PTT 测试。")), generalPage);
    voxHint->setWordWrap(true);
    form->addRow(voxHint);
    auto updatePTTFieldVisibility = [=]() {
        const int selectedMode = pttMode->currentIndex();
        const bool useHamlib = selectedMode == 1;
        const bool useSerial = selectedMode == 2 || selectedMode == 3;
        rigctl->setVisible(useHamlib);
        if (QWidget *label = form->labelForField(rigctl)) label->setVisible(useHamlib);
        serialRow->setVisible(useSerial);
        if (QWidget *label = form->labelForField(serialRow)) label->setVisible(useSerial);
        const bool softwarePTT = selectedMode != 0;
        activeHigh->setVisible(selectedMode == 2 || selectedMode == 3);
        lead->setVisible(softwarePTT);
        tail->setVisible(softwarePTT);
        if (QWidget *label = form->labelForField(lead)) label->setVisible(softwarePTT);
        if (QWidget *label = form->labelForField(tail)) label->setVisible(softwarePTT);
        voxHint->setVisible(selectedMode == 0);
    };
    connect(pttMode, QOverload<int>::of(&QComboBox::currentIndexChanged), &dialog,
            [updatePTTFieldVisibility](int) { updatePTTFieldVisibility(); });
    updatePTTFieldVisibility();
    generalLayout->addLayout(form);

    // Wails-compatible display settings: both RX and TX waterfalls share the
    // same frequency range, dB range, scrolling rate, history and palette.
    auto *displayPage = new QWidget(settingsTabs);
    auto *displayLayout = new QVBoxLayout(displayPage);
    auto *waterfallForm = new QFormLayout();
    QJsonObject waterfall = settings.value(QStringLiteral("waterfall")).toObject();
    if (waterfall.isEmpty()) {
        waterfall.insert(QStringLiteral("minFrequencyHz"), 0);
        waterfall.insert(QStringLiteral("maxFrequencyHz"), 5000);
        waterfall.insert(QStringLiteral("minDbfs"), -110);
        waterfall.insert(QStringLiteral("maxDbfs"), -20);
        waterfall.insert(QStringLiteral("rowsPerSecond"), 15);
        waterfall.insert(QStringLiteral("historySeconds"), 20);
        waterfall.insert(QStringLiteral("backgroundColor"), QStringLiteral("#07111b"));
        waterfall.insert(QStringLiteral("lowColor"), QStringLiteral("#153a78"));
        waterfall.insert(QStringLiteral("midColor"), QStringLiteral("#168fae"));
        waterfall.insert(QStringLiteral("highColor"), QStringLiteral("#39c6b0"));
        waterfall.insert(QStringLiteral("peakColor"), QStringLiteral("#f7d45b"));
    }
    auto addWaterfallSlider = [&](const QString &label, const QString &key, int min, int max, int fallback, const QString &suffix) {
        auto *row = new QWidget(displayPage);
        auto *rowLayout = new QHBoxLayout(row);
        rowLayout->setContentsMargins(0, 0, 0, 0);
        auto *slider = new QSlider(Qt::Horizontal, row);
        slider->setRange(min, max);
        slider->setValue(qBound(min, waterfall.value(key).toInt(fallback), max));
        auto *value = new QLabel(QString::number(slider->value()) + suffix, row);
        value->setMinimumWidth(78);
        value->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        rowLayout->addWidget(slider, 1);
        rowLayout->addWidget(value);
        connect(slider, &QSlider::valueChanged, value, [value, suffix](int next) { value->setText(QString::number(next) + suffix); });
        waterfallForm->addRow(label, row);
        return slider;
    };
    auto *minFrequency = addWaterfallSlider(uiText(QStringLiteral("最低频率")), QStringLiteral("minFrequencyHz"), 0, 47000, 0, QStringLiteral(" Hz"));
    auto *maxFrequency = addWaterfallSlider(uiText(QStringLiteral("最高频率")), QStringLiteral("maxFrequencyHz"), 100, 48000, 5000, QStringLiteral(" Hz"));
    auto *minDb = addWaterfallSlider(uiText(QStringLiteral("最低电平")), QStringLiteral("minDbfs"), -120, -10, -110, QStringLiteral(" dBFS"));
    auto *maxDb = addWaterfallSlider(uiText(QStringLiteral("最高电平")), QStringLiteral("maxDbfs"), -90, 0, -20, QStringLiteral(" dBFS"));
    auto *rowsPerSecond = addWaterfallSlider(uiText(QStringLiteral("滚动速度")), QStringLiteral("rowsPerSecond"), 1, 40, 15, uiText(QStringLiteral(" 行/秒")));
    auto *historySeconds = addWaterfallSlider(uiText(QStringLiteral("历史时长")), QStringLiteral("historySeconds"), 5, 60, 20, uiText(QStringLiteral(" 秒")));
    displayLayout->addLayout(waterfallForm);

    auto *paletteBox = new QGroupBox(uiText(QStringLiteral("瀑布图颜色")), displayPage);
    auto *paletteGrid = new QGridLayout(paletteBox);
    const QStringList colorKeys = {QStringLiteral("backgroundColor"), QStringLiteral("lowColor"), QStringLiteral("midColor"), QStringLiteral("highColor"), QStringLiteral("peakColor")};
    const QStringList colorNames = {uiText(QStringLiteral("背景色")), uiText(QStringLiteral("弱信号")), uiText(QStringLiteral("中等信号")), uiText(QStringLiteral("强信号")), uiText(QStringLiteral("峰值信号"))};
    QHash<QString, QPushButton *> colorButtons;
    for (int i = 0; i < colorKeys.size(); ++i) {
        auto *button = new QPushButton(colorNames[i], paletteBox);
        const QString initial = valueOr(waterfall, colorKeys[i], QStringLiteral("#07111b"));
        button->setProperty("color", initial);
        button->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(initial, QColor(initial).lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
        connect(button, &QPushButton::clicked, &dialog, [button, key=colorKeys[i], colorNames, i]() {
            QColor selected(button->property("color").toString());
            selected = QColorDialog::getColor(selected, button, colorNames[i]);
            if (!selected.isValid()) return;
            const QString hex = selected.name(QColor::HexRgb);
            button->setProperty("color", hex);
            button->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(hex, selected.lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
        });
        colorButtons.insert(colorKeys[i], button);
        paletteGrid->addWidget(button, i / 3, i % 3);
    }
    displayLayout->addWidget(paletteBox);
    auto *displayHint = new QLabel(uiText(QStringLiteral("横轴为频率，纵轴为时间；颜色代表信号强度。修改参数后点击保存应用到 RX 与 TX 两个瀑布图。")), displayPage);
    displayHint->setWordWrap(true);
    displayHint->setStyleSheet(QStringLiteral("font-size:8pt; color:#7d8991;"));
    displayLayout->addWidget(displayHint);
    auto *resetWaterfall = new QPushButton(uiText(QStringLiteral("恢复默认显示设置")), displayPage);
    displayLayout->addWidget(resetWaterfall, 0, Qt::AlignLeft);
    connect(resetWaterfall, &QPushButton::clicked, &dialog, [=]() {
        minFrequency->setValue(0); maxFrequency->setValue(5000);
        minDb->setValue(-110); maxDb->setValue(-20);
        rowsPerSecond->setValue(15); historySeconds->setValue(20);
        const QStringList defaults = {QStringLiteral("#07111b"), QStringLiteral("#153a78"), QStringLiteral("#168fae"), QStringLiteral("#39c6b0"), QStringLiteral("#f7d45b")};
        for (int i=0;i<colorKeys.size();++i) {
            auto *button = colorButtons.value(colorKeys[i]);
            const QString hex = defaults[i];
            button->setProperty("color", hex);
            button->setStyleSheet(QStringLiteral("background:%1; color:%2;").arg(hex, QColor(hex).lightness()<128?QStringLiteral("white"):QStringLiteral("black")));
        }
    });
    displayLayout->addStretch();
    settingsTabs->addTab(generalPage, uiText(QStringLiteral("设备与电台")));
    settingsTabs->addTab(displayPage, uiText(QStringLiteral("显示设置")));
    layout->addWidget(settingsTabs, 1);

    auto collectSettings = [=]() mutable {
        QJsonObject updated = settings;
        updated.insert(QStringLiteral("callsign"), callsign->text().trimmed());
        updated.insert(QStringLiteral("locator"), locator->text().trimmed());
        updated.insert(QStringLiteral("language"), language->currentIndex() == 1 ? QStringLiteral("en") : language->currentIndex() == 2 ? QStringLiteral("ja-JP") : QStringLiteral("zh-CN"));
        updated.insert(QStringLiteral("audioSampleRate"), sample->currentText().toInt());
        updated.insert(QStringLiteral("maryFecProfile"), maryFEC->currentIndex() == 1 ? QStringLiteral("robust") : QStringLiteral("balanced"));
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
        QJsonObject updatedWaterfall;
        updatedWaterfall.insert(QStringLiteral("minFrequencyHz"), minFrequency->value());
        updatedWaterfall.insert(QStringLiteral("maxFrequencyHz"), qMax(maxFrequency->value(), minFrequency->value() + 100));
        updatedWaterfall.insert(QStringLiteral("minDbfs"), minDb->value());
        updatedWaterfall.insert(QStringLiteral("maxDbfs"), qMax(maxDb->value(), minDb->value() + 5));
        updatedWaterfall.insert(QStringLiteral("rowsPerSecond"), rowsPerSecond->value());
        updatedWaterfall.insert(QStringLiteral("historySeconds"), historySeconds->value());
        for (const QString &key : colorKeys) updatedWaterfall.insert(key, colorButtons.value(key)->property("color").toString());
        updated.insert(QStringLiteral("waterfall"), updatedWaterfall);
        return updated;
    };

    auto *pttActions = new QHBoxLayout();
    auto *pttStart = new QPushButton(uiText(QStringLiteral("保存配置并测试 PTT")), &dialog);
    auto *pttStop = new QPushButton(uiText(QStringLiteral("停止 PTT 测试")), &dialog);
    pttActions->addWidget(pttStart);
    pttActions->addWidget(pttStop);
    pttActions->addStretch();
    generalLayout->addLayout(pttActions);
    auto updatePTTActionVisibility = [pttMode, pttStart, pttStop]() {
        const bool softwarePTT = pttMode->currentIndex() != 0;
        pttStart->setVisible(softwarePTT);
        pttStop->setVisible(softwarePTT);
    };
    connect(pttMode, QOverload<int>::of(&QComboBox::currentIndexChanged), &dialog,
            [updatePTTActionVisibility](int) { updatePTTActionVisibility(); });
    updatePTTActionVisibility();
    generalLayout->addStretch();
    connect(pttStart, &QPushButton::clicked, &dialog, [this, collectSettings]() mutable {
        const QJsonObject updated = collectSettings();
        QString saveError;
        m_backend->callSync(QStringLiteral("SaveSettings"), QJsonArray{updated}, &saveError, 15000);
        if (!saveError.isEmpty()) { showError(saveError); return; }
        m_settings = updated;
        const QJsonObject waterfallSettings = updated.value(QStringLiteral("waterfall")).toObject();
        if (m_rxSpectrum) m_rxSpectrum->setDisplaySettings(waterfallSettings);
        if (m_txSpectrum) m_txSpectrum->setDisplaySettings(waterfallSettings);
        m_backend->call(QStringLiteral("StartPTTTest"), {}, [this](bool ok, const QJsonValue &, const QString &pttError) {
            if (ok) appendLog(uiText(QStringLiteral("PTT 测试已启动"))); else showError(pttError);
        });
    });
    connect(pttStop, &QPushButton::clicked, &dialog, [this](bool) {
        m_backend->call(QStringLiteral("StopPTTTest"), {}, [this](bool ok, const QJsonValue &, const QString &pttError) {
            if (ok) appendLog(uiText(QStringLiteral("PTT 测试已停止"))); else showError(pttError);
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
    const QJsonObject waterfallSettings = updated.value(QStringLiteral("waterfall")).toObject();
    if (m_rxSpectrum) m_rxSpectrum->setDisplaySettings(waterfallSettings);
    if (m_txSpectrum) m_txSpectrum->setDisplaySettings(waterfallSettings);
    m_callsign->setText(callsign->text());
    m_locator->setText(locator->text());
    m_ax25SSID->setValue(updated.value(QStringLiteral("ax25Ssid")).toInt(m_ax25SSID->value()));
    const QString updatedLocale = updated.value(QStringLiteral("language")).toString(QStringLiteral("zh-CN"));
    if (m_languageSelect) {
        const QSignalBlocker blocker(m_languageSelect);
        m_languageSelect->setCurrentIndex(updatedLocale == QStringLiteral("en") ? 1 : updatedLocale.startsWith(QStringLiteral("ja")) ? 2 : 0);
    }
    applyLanguage(updatedLocale);
    refreshTemplateCanvas();
    appendLog(uiText(QStringLiteral("设备设置已保存")));
    refreshStatus();
}

void MainWindow::importGalleryImage() {
    const QString path=QFileDialog::getOpenFileName(this,uiText(QStringLiteral("导入图库图片")),QStandardPaths::writableLocation(QStandardPaths::PicturesLocation),QStringLiteral("Images (*.png *.jpg *.jpeg *.bmp *.webp)"));if(path.isEmpty())return;
    m_backend->call(QStringLiteral("ImportGalleryImage"),QJsonArray{path},[this](bool ok,const QJsonValue &value,const QString &error){if(!ok){showError(error);return;}const auto asset=value.toObject();appendLog(uiText(QStringLiteral("已导入图库："))+asset.value(QStringLiteral("name")).toString());m_templateImageCache.clear();refreshGallery();refreshInlineTemplateEditor();refreshTemplateCanvas();});
}
void MainWindow::exportAlbum() {
    const QString dir=QFileDialog::getExistingDirectory(this,uiText(QStringLiteral("导出相册到文件夹")),QStandardPaths::writableLocation(QStandardPaths::PicturesLocation));if(dir.isEmpty())return;
    m_backend->call(QStringLiteral("ExportAlbumToDirectoryPath"),QJsonArray{dir},[this](bool ok,const QJsonValue &value,const QString &error){if(!ok){showError(error);return;}QMessageBox::information(this,uiText(QStringLiteral("导出完成")),uiText(QStringLiteral("已导出 %1 条记录")).arg(value.toInt()));});
}
void MainWindow::exportTemplatePack() {
    const QString path=QFileDialog::getSaveFileName(this,uiText(QStringLiteral("导出模板包")),QStringLiteral("satssdv-templates.zip"),QStringLiteral("Template packs (*.zip)"));if(path.isEmpty())return;
    m_backend->call(QStringLiteral("ExportTemplatePackTo"),QJsonArray{path},[this, path](bool ok,const QJsonValue&,const QString &error){if(ok)appendLog(uiText(QStringLiteral("模板包已导出："))+path);else showError(error);});
}
void MainWindow::importTemplatePack() {
    const QString path=QFileDialog::getOpenFileName(this,uiText(QStringLiteral("导入模板包")),QString(),QStringLiteral("Template packs (*.zip)"));if(path.isEmpty())return;
    m_backend->call(QStringLiteral("ImportTemplatePackFrom"),QJsonArray{path},[this](bool ok,const QJsonValue &value,const QString &error){if(!ok){showError(error);return;}m_templateImageCache.clear();refreshGallery();refreshTemplates();refreshTemplateCanvas();QMessageBox::information(this,uiText(QStringLiteral("导入完成")),uiText(QStringLiteral("已导入 %1 个模板")).arg(value.toInt()));});
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
    for (int i = 0; i < components.size(); ++i) {
        QJsonObject component = components.at(i).toObject();
        const QString type = component.value(QStringLiteral("type")).toString();
        const bool gif = type == QStringLiteral("image")
            ? isGifGalleryAsset(m_gallery, component.value(QStringLiteral("assetId")).toString())
            : type == QStringLiteral("last-rx-image") && m_lastRXPreviewIsGIF;
        component.insert(QStringLiteral("isGIF"), gif);
        components.replace(i, component);
    }

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
    dialog.setWindowTitle(uiText(QStringLiteral("模板可视化编辑器")));
    dialog.resize(1260, 820);
    auto *root = new QVBoxLayout(&dialog);
    auto *nameRow = new QHBoxLayout();
    nameRow->addWidget(new QLabel(uiText(QStringLiteral("模板名称")), &dialog));
    auto *nameEdit = new QLineEdit(selected.value(QStringLiteral("name")).toString(), &dialog);
    nameEdit->setMaxLength(48);
    nameRow->addWidget(nameEdit, 1);
    root->addLayout(nameRow);

    auto *toolbar = new QHBoxLayout();
    auto *addText = new QPushButton(uiText(QStringLiteral("＋ 文字")), &dialog);
    auto *addImage = new QPushButton(uiText(QStringLiteral("＋ 图库图像")), &dialog);
    auto *addLastRx = new QPushButton(uiText(QStringLiteral("＋ 上次接收图像")), &dialog);
    auto *moveLayerUp = new QPushButton(uiText(QStringLiteral("上移图层")), &dialog);
    auto *moveLayerDown = new QPushButton(uiText(QStringLiteral("下移图层")), &dialog);
    auto *snapGrid = new QCheckBox(uiText(QStringLiteral("5% 网格吸附")), &dialog);
    auto *deleteComponent = new QPushButton(uiText(QStringLiteral("删除选中项")), &dialog);
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
    propertiesLayout->addWidget(new QLabel(uiText(QStringLiteral("属性")), properties));
    auto *table = new QTableWidget(properties);
    table->setColumnCount(5);
    table->setHorizontalHeaderLabels({uiText(QStringLiteral("文字内容")), QStringLiteral("X %"), QStringLiteral("Y %"), uiText(QStringLiteral("字号")), uiText(QStringLiteral("颜色"))});
    table->horizontalHeader()->setSectionResizeMode(0, QHeaderView::Stretch);
    for (int column = 1; column < 5; ++column) table->horizontalHeader()->setSectionResizeMode(column, QHeaderView::ResizeToContents);
    table->setSelectionBehavior(QAbstractItemView::SelectRows);
    table->setAlternatingRowColors(true);
    propertiesLayout->addWidget(table, 1);
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
        component.insert(QStringLiteral("text"), uiText(QStringLiteral("新文本")));
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
            QMessageBox::information(&dialog, uiText(QStringLiteral("图库为空")), uiText(QStringLiteral("请先导入图库图像。")));
            return;
        }
        bool ok = false;
        const QString choice = QInputDialog::getItem(&dialog, uiText(QStringLiteral("选择图像")), uiText(QStringLiteral("选择图库图像")), labels, 0, false, &ok);
        if (!ok) return;
        const int selectedAsset = labels.indexOf(choice);
        if (selectedAsset < 0) return;
        QJsonObject component;
        const QString componentId = QUuid::createUuid().toString(QUuid::WithoutBraces);
        component.insert(QStringLiteral("id"), componentId);
        component.insert(QStringLiteral("type"), QStringLiteral("image"));
        component.insert(QStringLiteral("assetId"), ids[selectedAsset]);
        const bool gif = isGifGalleryAsset(m_gallery, ids[selectedAsset]);
        component.insert(QStringLiteral("isGIF"), gif);
        component.insert(QStringLiteral("x"), 32.0);
        component.insert(QStringLiteral("y"), 28.0);
        component.insert(QStringLiteral("scale"), 35.0);
        component.insert(QStringLiteral("visible"), true);
        component.insert(QStringLiteral("outline"), true);
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
        component.insert(QStringLiteral("outline"), true);
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
            showError(uiText(QStringLiteral("模板位置必须为 0–100%，字号必须为 6–160 px。")));
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
        showError(uiText(QStringLiteral("模板名称不能为空。")));
        return;
    }
    selected.insert(QStringLiteral("name"), name);
    selected.insert(QStringLiteral("components"), components);
    m_templates.replace(templateIndex, selected);
    m_backend->call(QStringLiteral("SaveTemplates"), QJsonArray{m_templates}, [this](bool ok, const QJsonValue &, const QString &error) {
        if (!ok) { showError(error); return; }
        appendLog(uiText(QStringLiteral("模板布局已保存")));
        refreshTemplates();
        const QImage image = renderTemplate();
        if (!image.isNull()) m_txPreview->setSourceImage(image);
    });
}

void MainWindow::selectTemplateBackground() {
    selectInlineGalleryAsset();
}

void MainWindow::showImage(const QString &dataUrl, ImagePreviewLabel *target) {
    if (!target || dataUrl.isEmpty()) return;
    if (dataUrl.startsWith(QStringLiteral("data:image/gif"), Qt::CaseInsensitive)) {
        const qsizetype separator = dataUrl.indexOf(QLatin1Char(','));
        if (separator >= 0) {
            const QByteArray gifData = QByteArray::fromBase64(dataUrl.mid(separator + 1).toLatin1());
            QString gifError;
            if (target->setSourceGIF(gifData, &gifError)) return;
            const QString message = uiText(QStringLiteral("GIF 预览播放失败：%1")).arg(gifError);
            appendLog(message);
            if (target == m_rxPreview) statusBar()->showMessage(message, 10000);
        }
    }
    const QImage image = imageFromDataUrl(dataUrl);
    if (!image.isNull()) target->setSourceImage(image);
}
void MainWindow::appendLog(const QString &text){if(m_log){m_log->append(QStringLiteral("[%1] %2").arg(QDateTime::currentDateTime().toString(QStringLiteral("HH:mm:ss.zzz")),text));}}
void MainWindow::showError(const QString &message) {
    appendLog(uiText(QStringLiteral("错误：")) + message);
    statusBar()->showMessage(message, 10000);
    if (m_audioStatus) m_audioStatus->setToolTip(message);
}
