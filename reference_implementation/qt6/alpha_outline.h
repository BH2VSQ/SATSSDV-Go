#pragma once

#include <QColor>
#include <QImage>
#include <QPainter>
#include <QRectF>

void drawAlphaContourOutline(QPainter &painter, const QImage &image,
                             const QRectF &target, const QColor &color,
                             qreal outlineWidth);
