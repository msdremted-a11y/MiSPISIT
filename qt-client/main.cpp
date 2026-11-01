#include <QApplication>
#include <QWidget>
#include <QVBoxLayout>
#include <QLabel>
#include <QFont>

int main(int argc, char *argv[])
{
    QApplication app(argc, argv);

    QWidget window;
    window.setWindowTitle("Системный монитор на базе eBPF + Qt");
    window.resize(700, 400);

    auto *layout = new QVBoxLayout(&window);
    layout->setAlignment(Qt::AlignCenter);
    layout->setSpacing(15);

    // Блок 1: Название
    auto *title = new QLabel("Системный монитор на базе eBPF + Qt");
    title->setAlignment(Qt::AlignCenter);
    QFont titleFont = title->font();
    titleFont.setPointSize(18);
    titleFont.setBold(true);
    title->setFont(titleFont);

    // Блок 2: Описание
    auto *description = new QLabel(
        "Инструмент для мониторинга системных вызовов и сетевой активности "
        "в Linux с использованием eBPF. Предназначен для системных "
        "администраторов и разработчиков, желающих отслеживать поведение "
        "приложений в реальном времени."
    );
    description->setAlignment(Qt::AlignCenter);
    description->setWordWrap(true);
    QFont descFont = description->font();
    descFont.setPointSize(11);
    description->setFont(descFont);

    // Блок 3: Версия
    auto *version = new QLabel("Версия 0.0.1");
    version->setAlignment(Qt::AlignCenter);
    QFont versionFont = version->font();
    versionFont.setPointSize(14);
    version->setFont(versionFont);
    version->setStyleSheet("color: gray;");

    // Блок 4: Статус
    auto *status = new QLabel("Разработка продолжается");
    status->setAlignment(Qt::AlignCenter);
    QFont statusFont = status->font();
    statusFont.setPointSize(12);
    status->setFont(statusFont);
    status->setStyleSheet("color: #888888;");

    layout->addWidget(title);
    layout->addSpacing(20);
    layout->addWidget(description);
    layout->addSpacing(20);
    layout->addWidget(version);
    layout->addSpacing(10);
    layout->addWidget(status);

    window.show();
    return app.exec();
}
