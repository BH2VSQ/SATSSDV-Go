#pragma once

#include <QString>
#include <QWidget>

QString uiText(const QString &source);
void setUiLocale(const QString &locale);
void translateWidgetTree(QWidget *root);
