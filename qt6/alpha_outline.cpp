#include "alpha_outline.h"

#include <QVector>
#include <QtMath>
#include <algorithm>
#include <cmath>

void drawAlphaContourOutline(QPainter &painter, const QImage &image,
                             const QRectF &target, const QColor &color,
                             qreal outlineWidth) {
    if (image.isNull() || target.isEmpty()) return;
    const int radius = qBound(0, qCeil(outlineWidth), 20);
    if (radius <= 0) { painter.drawImage(target, image); return; }

    const int sourceWidth = qMax(1, qCeil(target.width()));
    const int sourceHeight = qMax(1, qCeil(target.height()));
    const QImage scaled = image.scaled(sourceWidth, sourceHeight, Qt::IgnoreAspectRatio, Qt::SmoothTransformation)
                              .convertToFormat(QImage::Format_ARGB32);
    const int padding = radius + 1;
    const int outputWidth = sourceWidth + padding * 2;
    const int outputHeight = sourceHeight + padding * 2;
    QVector<quint8> sourceAlpha(sourceWidth * sourceHeight);
    for (int y = 0; y < sourceHeight; ++y) {
        const QRgb *row = reinterpret_cast<const QRgb *>(scaled.constScanLine(y));
        for (int x = 0; x < sourceWidth; ++x)
            sourceAlpha[y * sourceWidth + x] = static_cast<quint8>(qAlpha(row[x]));
    }

    QVector<quint8> expanded(outputWidth * outputHeight, 0);
    QVector<int> deque(sourceWidth + 1);
    // Circular dilation, using a sliding maximum for each horizontal span.
    // This matches the Wails alpha-mask algorithm without drawing a rectangular
    // border around transparent PNG padding.
    for (int dy = -radius; dy <= radius; ++dy) {
        const int dx = static_cast<int>(std::floor(std::sqrt(double(radius * radius - dy * dy))));
        for (int sy = 0; sy < sourceHeight; ++sy) {
            const int targetY = padding + sy + dy;
            if (targetY < 0 || targetY >= outputHeight) continue;
            const int sourceOffset = sy * sourceWidth;
            const int targetOffset = targetY * outputWidth;
            int head = 0, tail = 0, next = 0;
            for (int targetX = 0; targetX < outputWidth; ++targetX) {
                const int right = qMin(sourceWidth - 1, targetX - padding + dx);
                while (next <= right) {
                    const quint8 value = sourceAlpha[sourceOffset + next];
                    while (tail > head && sourceAlpha[sourceOffset + deque[tail - 1]] <= value) --tail;
                    deque[tail++] = next++;
                }
                const int left = targetX - padding - dx;
                while (head < tail && deque[head] < left) ++head;
                if (head < tail) {
                    const quint8 value = sourceAlpha[sourceOffset + deque[head]];
                    const int index = targetOffset + targetX;
                    if (value > expanded[index]) expanded[index] = value;
                }
            }
        }
    }

    QImage mask(outputWidth, outputHeight, QImage::Format_ARGB32);
    mask.fill(Qt::transparent);
    for (int y = 0; y < outputHeight; ++y) {
        QRgb *row = reinterpret_cast<QRgb *>(mask.scanLine(y));
        for (int x = 0; x < outputWidth; ++x) {
            const int alpha = expanded[y * outputWidth + x];
            if (alpha > 0) row[x] = qRgba(color.red(), color.green(), color.blue(), alpha);
        }
    }
    const qreal sx = target.width() / qMax(1, sourceWidth);
    const qreal sy = target.height() / qMax(1, sourceHeight);
    const QRectF maskRect(target.x() - padding * sx, target.y() - padding * sy,
                          outputWidth * sx, outputHeight * sy);
    painter.drawImage(maskRect, mask);
    painter.drawImage(target, image);
}
