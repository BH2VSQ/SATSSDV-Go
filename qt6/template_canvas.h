#pragma once

#include <QHash>
#include <QImage>
#include <QJsonArray>
#include <QJsonObject>
#include <QWidget>
#include <QPointF>
#include <QRectF>
#include <functional>
#include <utility>

class QKeyEvent;
class QMouseEvent;
class QPaintEvent;

class TemplateCanvas final : public QWidget {
public:
    using TextResolver = std::function<QString(const QString &)>;
    using ComponentsChanged = std::function<void(const QJsonArray &)>;
    using SelectionChanged = std::function<void(int)>;

    explicit TemplateCanvas(QWidget *parent = nullptr);

    void setDocument(const QJsonArray &components,
                     const QImage &background,
                     const QHash<QString, QImage> &componentImages,
                     TextResolver resolver);
    QJsonArray components() const { return m_components; }
    int selectedIndex() const { return m_selected; }
    void setSelectedIndex(int index);
    void setComponentsChangedCallback(ComponentsChanged callback) { m_componentsChanged = std::move(callback); }
    void setSelectionChangedCallback(SelectionChanged callback) { m_selectionChanged = std::move(callback); }
    void removeSelectedComponent();
    void moveSelectedLayerUp();
    void moveSelectedLayerDown();
    void setSnapToGrid(bool enabled);

    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    void mouseMoveEvent(QMouseEvent *event) override;
    void mouseReleaseEvent(QMouseEvent *event) override;
    void keyPressEvent(QKeyEvent *event) override;

private:
    enum class DragMode { None, Move, Resize };

    QRectF canvasRect() const;
    QRectF componentRect(int index) const;
    int hitTest(const QPointF &position) const;
    QRectF handleRect(const QRectF &component) const;
    void notifyComponentsChanged();
    void moveSelectedLayer(int direction);

    QJsonArray m_components;
    QImage m_background;
    QHash<QString, QImage> m_componentImages;
    TextResolver m_textResolver;
    ComponentsChanged m_componentsChanged;
    SelectionChanged m_selectionChanged;
    int m_selected = -1;
    bool m_snapToGrid = false;
    DragMode m_dragMode = DragMode::None;
    QPointF m_pressPosition;
    QJsonObject m_originalComponent;
};
