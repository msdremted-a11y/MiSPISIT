# Системный монитор на базе eBPF + Qt

Инструмент для мониторинга системных вызовов и сетевой активности
в Linux с использованием eBPF.

## Запуск

Разрешить Docker доступ к X-серверу:
    xhost +local:docker

Скачать и запустить образ:

    docker pull troykins/qt-ebpf-monitor:0.0.1
    docker run --rm -it -e DISPLAY=$DISPLAY -v /tmp/.X11-unix:/tmp/.X11-unix troykins/qt-ebpf-monitor:0.0.1
