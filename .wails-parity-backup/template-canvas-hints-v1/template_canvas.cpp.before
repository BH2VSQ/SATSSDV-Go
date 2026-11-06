#include "template_canvas.h"
#include "alpha_outline.h"
#include "ui_i18n.h"

#include <QFont>
#include <QFontMetricsF>
#include <QKeyEvent>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QSizePolicy>
#include <QStringList>
#include <QPen>
#include <QtMath>
#include <algorithm>

namespace {
constexpr qreal kCanvasWidth = 640.0;
constexpr qreal kCanvasHeight = 480.0;

QString componentType(const QJsonObject &component) {
    return component.value(QStringLiteral("type")).toString();
}

qreal componentX(const QJsonObject &component) {
    return component.value(QStringLiteral("x")).toDouble() * kCanvasWidth / 100.0;
}

qreal componentY(const QJsonObject &component) {
    return component.value(QStringLiteral("y")).toDouble() * kCanvasHeight / 100.0;
}

QFont componentFont(const QJsonObject &component) {
    QFont font(QStringLiteral("Segoe UI"));
    font.setPixelSize(qBound(6, qRound(component.value(QStringLiteral("fontSize")).toDouble(24.0)), 160));
    font.setBold(component.value(QStringLiteral("bold")).toBool());
    return font;
}
}

TemplateCanvas::TemplateCanvas(QWidget *parent) : QWidget(parent) {
    setMinimumSize(420, 315);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    setFocusPolicy(Qt::StrongFocus);
    setMouseTracking(true);
    setToolTip(uiText(QStringLiteral("拖动组件移动；拖动右下角控制点调整字号或图像比例；选中后按 Delete 删除")));
}

void TemplateCanvas::setDocument(const QJsonArray &components,
                                 const QImage &background,
                                 const QHash<QString, QImage> &componentImages,
                                 TextResolver resolver) {
    m_components = components;
    m_background = background;
    m_componentImages = componentImages;
    m_textResolver = std::move(resolver);
    if (m_selected >= m_components.size()) m_selected = -1;
    update();
}

void TemplateCanvas::setSelectedIndex(int index) {
    const int next = (index >= 0 && index < m_components.size()) ? index : -1;
    if (m_selected == next) return;
    m_selected = next;
    update();
    if (m_selectionChanged) m_selectionChanged(m_selected);
}

QSize TemplateCanvas::sizeHint() const {
    return QSize(640, 480);
}

QRectF TemplateCanvas::canvasRect() const {
    const qreal availableWidth = qMax(1, width() - 20);
    const qreal availableHeight = qMax(1, height() - 20);
    const qreal scale = qMin(availableWidth / kCanvasWidth, availableHeight / kCanvasHeight);
    const QSizeF size(kCanvasWidth * scale, kCanvasHeight * scale);
    return QRectF((width() - size.width()) / 2.0,
                  (height() - size.height()) / 2.0,
                  size.width(), size.height());
}

QRectF TemplateCanvas::componentRect(int index) const {
    if (index < 0 || index >= m_components.size()) return {};
    const QJsonObject component = m_components.at(index).toObject();
    const QString type = componentType(component);
    const qreal x = componentX(component);
    const qreal y = componentY(component);

    if (type == QStringLiteral("text")) {
        const QFont font = componentFont(component);
        const QFontMetricsF metrics(font);
        QString text = component.value(QStringLiteral("text")).toString();
        if (m_textResolver) text = m_textResolver(text);
        const QStringList lines = text.split(QLatin1Char('\n'));
        qreal maximumWidth = 1.0;
        for (const QString &line : lines) maximumWidth = qMax(maximumWidth, metrics.horizontalAdvance(line));
        const qreal height = qMax<qreal>(font.pixelSize(), lines.size() * metrics.height() * 1.22);
        return QRectF(x - 4.0, y - 3.0, maximumWidth + 12.0, height + 8.0);
    }

    if (type == QStringLiteral("image") || type == QStringLiteral("last-rx-image")) {
        const QString id = component.value(QStringLiteral("id")).toString();
        const QImage image = m_componentImages.value(id);
        const qreal scale = qBound(1.0, component.value(QStringLiteral("scale")).toDouble(35.0), 100.0) / 100.0;
        if (image.isNull()) return QRectF(x, y, kCanvasWidth * 0.35 * scale, kCanvasHeight * 0.35 * scale);
        const qreal fit = qMin(kCanvasWidth / image.width(), kCanvasHeight / image.height());
        return QRectF(x, y, image.width() * fit * scale, image.height() * fit * scale);
    }
    return {};
}

QRectF TemplateCanvas::handleRect(const QRectF &component) const {
    const QRectF canvas = canvasRect();
    const qreal scale = canvas.width() / kCanvasWidth;
    const qreal size = 12.0;
    const QPointF bottomRight(canvas.left() + component.right() * scale,
                              canvas.top() + component.bottom() * scale);
    return QRectF(bottomRight.x() - size / 2.0, bottomRight.y() - size / 2.0, size, size);
}

int TemplateCanvas::hitTest(const QPointF &position) const {
    const QRectF canvas = canvasRect();
    if (!canvas.contains(position)) return -1;
    const qreal scale = canvas.width() / kCanvasWidth;
    for (int i = m_components.size() - 1; i >= 0; --i) {
        const QJsonObject component = m_components.at(i).toObject();
        if (component.contains(QStringLiteral("visible")) && !component.value(QStringLiteral("visible")).toBool()) continue;
        const QRectF rect = componentRect(i);
        const QRectF screenRect(canvas.left() + rect.left() * scale,
                                canvas.top() + rect.top() * scale,
                                rect.width() * scale,
                                rect.height() * scale);
        if (screenRect.adjusted(-4, -4, 4, 4).contains(position)) return i;
    }
    return -1;
}

void TemplateCanvas::paintEvent(QPaintEvent *) {
    QPainter painter(this);
    painter.fillRect(rect(), QColor(QStringLiteral("#0b1218")));
    const QRectF target = canvasRect();
    painter.fillRect(target, QColor(QStringLiteral("#17232d")));
    if (!m_background.isNull()) painter.drawImage(target, m_background);

    painter.save();
    painter.setClipRect(target);
    painter.translate(target.topLeft());
    const qreal scale = target.width() / kCanvasWidth;
    painter.scale(scale, scale);

    if (m_snapToGrid) {
        painter.save();
        painter.setPen(QPen(QColor(83, 215, 239, 44), 0.8, Qt::DotLine));
        for (int x = 32; x < int(kCanvasWidth); x += 32)
            painter.drawLine(QPointF(x, 0), QPointF(x, kCanvasHeight));
        for (int y = 24; y < int(kCanvasHeight); y += 24)
            painter.drawLine(QPointF(0, y), QPointF(kCanvasWidth, y));
        painter.restore();
    }

    for (int index = 0; index < m_components.size(); ++index) {
        const QJsonObject component = m_components.at(index).toObject();
        if (component.contains(QStringLiteral("visible")) && !component.value(QStringLiteral("visible")).toBool()) continue;
        const QString type = componentType(component);
        const qreal x = componentX(component);
        const qreal y = componentY(component);
        if (type == QStringLiteral("text")) {
            const QFont font = componentFont(component);
            painter.setFont(font);
            const QString rawText = component.value(QStringLiteral("text")).toString();
            const QString text = m_textResolver ? m_textResolver(rawText) : rawText;
            const QColor fill(component.value(QStringLiteral("color")).toString(QStringLiteral("#ffffff")));
            const QColor outline(component.value(QStringLiteral("outlineColor")).toString(QStringLiteral("#000000")));
            const qreal outlineWidth = qBound(0.0, component.value(QStringLiteral("outlineWidth")).toDouble(2.0), 20.0);
            const QStringList lines = text.split(QLatin1Char('\n'));
            qreal baseline = y + font.pixelSize();
            for (const QString &line : lines) {
                if (component.value(QStringLiteral("outline")).toBool() && outlineWidth > 0.0) {
                    QPainterPath path;
                    path.addText(QPointF(x, baseline), font, line);
                    painter.strokePath(path, QPen(outline, outlineWidth * 2.0, Qt::SolidLine, Qt::RoundCap, Qt::RoundJoin));
                    painter.fillPath(path, fill);
                } else {
                    painter.setPen(fill);
                    painter.drawText(QPointF(x, baseline), line);
                }
                baseline += font.pixelSize() * 1.22;
            }
        } else if (type == QStringLiteral("image") || type == QStringLiteral("last-rx-image")) {
            const QString id = component.value(QStringLiteral("id")).toString();
            const QImage image = m_componentImages.value(id);
            if (image.isNull()) {
                const QRectF placeholder(x, y, kCanvasWidth * 0.16, kCanvasHeight * 0.12);
                painter.fillRect(placeholder, QColor(22, 38, 50, 220));
                painter.setPen(QColor(QStringLiteral("#c5d7df")));
                painter.drawRect(placeholder);
                painter.drawText(placeholder, Qt::AlignCenter,
                                 type == QStringLiteral("last-rx-image") ? QStringLiteral("LAST RX") : QStringLiteral("IMAGE"));
            } else {
                const qreal fit = qMin(kCanvasWidth / image.width(), kCanvasHeight / image.height());
                const qreal factor = fit * qBound(0.01, component.value(QStringLiteral("scale")).toDouble(35.0) / 100.0, 1.0);
                const QRectF imageRect(x, y, image.width() * factor, image.height() * factor);
                const QColor imageOutline(component.value(QStringLiteral("outlineColor")).toString(QStringLiteral("#000000")));
                const qreal imageOutlineWidth = qBound(0.0, component.value(QStringLiteral("outlineWidth")).toDouble(2.0), 20.0);
                if (component.value(QStringLiteral("outline")).toBool(true) && imageOutlineWidth > 0) {
                    drawAlphaContourOutline(painter, image, imageRect, imageOutline, imageOutlineWidth);
                } else {
                    painter.drawImage(imageRect, image);
                }
            }
        }

        if (index == m_selected) {
            const QRectF selected = componentRect(index);
            painter.setPen(QPen(QColor(QStringLiteral("#53d7ef")), 1.4 / scale, Qt::DashLine));
            painter.setBrush(Qt::NoBrush);
            painter.drawRect(selected);
            painter.setBrush(QColor(QStringLiteral("#53d7ef")));
            painter.setPen(QPen(QColor(QStringLiteral("#07111b")), 1.0 / scale));
            painter.drawRect(QRectF(selected.right() - 4.5 / scale, selected.bottom() - 4.5 / scale,
                                    9.0 / scale, 9.0 / scale));
        }
    }
    painter.restore();

    painter.setPen(QColor(QStringLiteral("#718896")));
    painter.drawText(QRectF(8, height() - 22, width() - 16, 16), Qt::AlignRight | Qt::AlignVCenter,
                     uiText(QStringLiteral("640 × 480")));
}

void TemplateCanvas::mousePressEvent(QMouseEvent *event) {
    if (event->button() != Qt::LeftButton) {
        QWidget::mousePressEvent(event);
        return;
    }
    setFocus(Qt::MouseFocusReason);
    const QPointF position = event->position();
    if (m_selected >= 0) {
        const QRectF rect = componentRect(m_selected);
        if (handleRect(rect).contains(position)) {
            m_dragMode = DragMode::Resize;
            m_pressPosition = position;
            m_originalComponent = m_components.at(m_selected).toObject();
            event->accept();
            return;
        }
    }
    const int hit = hitTest(position);
    setSelectedIndex(hit);
    if (hit >= 0) {
        m_dragMode = DragMode::Move;
        m_pressPosition = position;
        m_originalComponent = m_components.at(hit).toObject();
        setCursor(Qt::ClosedHandCursor);
        event->accept();
        return;
    }
    m_dragMode = DragMode::None;
    unsetCursor();
    QWidget::mousePressEvent(event);
}

void TemplateCanvas::mouseMoveEvent(QMouseEvent *event) {
    if (m_dragMode == DragMode::None || m_selected < 0) {
        const int hit = hitTest(event->position());
        if (hit >= 0) setCursor(Qt::OpenHandCursor); else unsetCursor();
        QWidget::mouseMoveEvent(event);
        return;
    }
    const QRectF canvas = canvasRect();
    if (canvas.width() <= 1.0 || canvas.height() <= 1.0) return;
    const QPointF delta = event->position() - m_pressPosition;
    QJsonObject component = m_originalComponent;
    if (m_dragMode == DragMode::Move) {
        qreal x = m_originalComponent.value(QStringLiteral("x")).toDouble() + delta.x() / canvas.width() * 100.0;
        qreal y = m_originalComponent.value(QStringLiteral("y")).toDouble() + delta.y() / canvas.height() * 100.0;
        if (m_snapToGrid) {
            x = qRound(x / 5.0) * 5.0;
            y = qRound(y / 5.0) * 5.0;
        }
        component.insert(QStringLiteral("x"), x);
        component.insert(QStringLiteral("y"), y);
    } else {
        const QString type = componentType(component);
        if (type == QStringLiteral("text")) {
            const qreal original = m_originalComponent.value(QStringLiteral("fontSize")).toDouble(24.0);
            const qreal adjusted = original + delta.y() / canvas.height() * kCanvasHeight;
            component.insert(QStringLiteral("fontSize"), qBound(6.0, adjusted, 160.0));
        } else {
            const qreal original = m_originalComponent.value(QStringLiteral("scale")).toDouble(35.0);
            const qreal adjusted = original + delta.x() / canvas.width() * 100.0;
            component.insert(QStringLiteral("scale"), qBound(1.0, adjusted, 100.0));
        }
    }
    m_components.replace(m_selected, component);
    update();
    notifyComponentsChanged();
    event->accept();
}

void TemplateCanvas::mouseReleaseEvent(QMouseEvent *event) {
    if (event->button() == Qt::LeftButton && m_dragMode != DragMode::None) {
        m_dragMode = DragMode::None;
        unsetCursor();
        event->accept();
        return;
    }
    QWidget::mouseReleaseEvent(event);
}

void TemplateCanvas::keyPressEvent(QKeyEvent *event) {
    if (event->key() == Qt::Key_Delete || event->key() == Qt::Key_Backspace) {
        removeSelectedComponent();
        event->accept();
        return;
    }
    QWidget::keyPressEvent(event);
}

void TemplateCanvas::removeSelectedComponent() {
    if (m_selected < 0 || m_selected >= m_components.size()) return;
    m_components.removeAt(m_selected);
    m_selected = -1;
    m_dragMode = DragMode::None;
    update();
    if (m_selectionChanged) m_selectionChanged(-1);
    notifyComponentsChanged();
}

void TemplateCanvas::notifyComponentsChanged() {
    if (m_componentsChanged) m_componentsChanged(m_components);
}

void TemplateCanvas::moveSelectedLayerUp() {
    moveSelectedLayer(1);
}

void TemplateCanvas::moveSelectedLayerDown() {
    moveSelectedLayer(-1);
}

void TemplateCanvas::moveSelectedLayer(int direction) {
    if (m_selected < 0 || m_selected >= m_components.size()) return;
    const int target = m_selected + direction;
    if (target < 0 || target >= m_components.size()) return;

    const QJsonValue selected = m_components.at(m_selected);
    m_components.removeAt(m_selected);
    m_components.insert(target, selected);
    m_selected = target;
    update();
    // Update the property table's model indexes before notifying selection.
    notifyComponentsChanged();
    if (m_selectionChanged) m_selectionChanged(m_selected);
}

void TemplateCanvas::setSnapToGrid(bool enabled) {
    if (m_snapToGrid == enabled) return;
    m_snapToGrid = enabled;
    update();
}
