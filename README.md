# Tarantool testcontainers module

Реализация модуля библиотеки testcontainers для работы с docker образами БД tarantool

## Примеры использования
```go
        // Время ожидания готовности сервера БД принимать запросы
        waitStartTimeout := 5*time.Second
        // Создает и запускает контейнер с выбранным образом тарантула версии
        container, err := RunContainer(ctx, 
            WithTarantool15("tarantool/tarantool:2.10", waitStartTimeout),
        )
        if err != nil {
            return fmt.Errorf("failed to run container: %w", err)
        }
        ...
        // Получает строку соединения вида host:port для доступа в сервер БД в запущенном контейнере
        dbConnectionStr, err := container.ServerHostPort()
        ...
        // Удаляет контейнер
        if err := container.Terminate(ctx); err != nil {
            return fmt.Errorf("failed to terminate container: %w", err)
        }
```

```go
        // Время ожидания готовности сервера БД принимать запросы
        waitStartTimeout := 5*time.Second
        // Путь к файлу конфигурации тарантула
        configPath := ".../tarantool15.cfg"
        // Путь к файлам с луа функциями
        scriptsPath := "./"
        // Реализация методов логера testcontainers.Logging
        logger := MyLogger()
        // Создает контейнер
        container, err := NewContainer(ctx, 
			// с образом тарантула версии
            WithTarantool15("tarantool/tarantool:1.5", waitStartTimeout),
            // с файлом конфигурации тарантула монтируемым в контейне по пути /etc/tarantool/tarantool.cfg
			WithConfigFile(configPath, "/etc/tarantool/tarantool.cfg"),
            // с папкой скриптов содержищих луакод монтируемым в контейне по пути /opt/luascript/
            WithScriptsMapping(scriptsPath, "/opt/luascript/"),
			// со сбором логов из контейнера
            WithLogger(logger),
        )
        if err != nil {
            return fmt.Errorf("failed to create container: %w", err)
        }

        // Запускает контейнер
        if err := c.StartContainer(ctx); err != nil {
            return fmt.Errorf("can't start container: %w", err)
        }
        ...
        // Получает строку соединения вида host:port для доступа в сервер БД в запущенном контейнере
        dbConnectionStr, err := container.ServerHostPort()
        ...
        // Останавливает контейнер
        if err := container.Stop(ctx); err != nil {
            return fmt.Errorf("failed to terminate container: %w", err)
        }
}
```

## Доступные опции

- ``WithTarantool15(image string, waitTimeout time.Duration)`` - ожидает в течении `waitTimeout` готовность сервера БД tarantool версии 1.5 поднимаемого из образа `image`
- ``WithTarantool2(image string, waitTimeout time.Duration)`` - ожидает в течении `waitTimeout` готовность сервера БД tarantool версий 1.6-2.11 поднимаемого из образа `image`
- ``WithCommand(cmd []string)`` - переопределяет поведения при запуске контейнера, выполняя команды cmd
- ``WithArguments(args ...string)`` - запуск контейнера tarantool с аргументами для entrypoint, например для образа tarantool/tarantool:2.11 в качестве аргумента можно передать путь к файлу с lua скриптом, который будет выполнен после старта контейнера
- ``WithScripts(hostPath string, containerPath string)`` - копирует папку с lua скриптами в контейнер перед запуском контейнера, используется, когда нет возможности монтировать из файловой системы
- ``WithConfigFileMapping(cfgPath, containerCfgPath string)`` - монтирует файл расположенной в cfgPath в контейнер по пути containerCfgPath
- ``WithName(name string, reuse bool)`` - задает фиксированное имя для контейнера и возможность переиспользовать уже существующий контейнер с этим именем
- ``WithEnv(prefix string)`` - копирует в контейнер переменные окружения начинающиеся с prefix, например WithEnv("TARANTOOL_")
- ``WithHookOnStartTarantool(hook func(ctx context.Context, connection string) error)`` - регистрирует функцию, которая будет выполнена после запуска в контейнере сервера БД. В функцию передается строка соединения вида `host:port` для доступа в сервер БД
- ``WithHookOnStopTarantool(hook func(ctx context.Context, connection string) error)``  - регистрирует функцию, которая будет выполнена перед остановкой в контейнере сервера БД. В функцию передается строка соединения вида `host:port` для доступа в сервер БД
## Дополнительные методы работы с контейнером

- ``CopyDirToContainer(hostDirPath string, containerParentPath string, fileMode int64)`` - обертка над оригинальным методом `testcontainers.Container` позволяет копировать файлы
  с хост машины, включая содержимое символических ссылок на файлы. Используется, когда нет возможности монтировать из файловой системы

## Особенности работы с оркестраторами контейнеров на удаленных хостах

Необходимо настроить переменную окружения DOCKER_HOST, см. [детали конфигурирования](https://golang.testcontainers.org/features/configuration/#docker-host-detection)

### Настройки для оркестраторов контейнеров поднятых на виртуальных машинах [lima](https://lima-vm.io/docs/)/[colima](https://github.com/abiosoft/colima)
Необходимо для запуска контейнеров x86 архитектуры на macos M1)

Настроить переменные окружения: 

- DOCKER_HOST=$(limactl list docker --format 'unix://{{.Dir}}/sock/docker.sock'), например DOCKER_HOST="unix://${HOME}/.lima/default/sock/docker.sock"
- TT_HOST=ip по которой доступна гостевая ОС на хосте (необходимо предварительно настроить [сеть](https://lima-vm.io/docs/config/network/) )
- TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=unix:///var/run/docker.sock

В случае медленных виртуальных эмуляторов сетевых интерфейсов (например qemu) контейнер может быть не доступен сразу для соединения с хостмашины. 
В этом случае можно установить переменную окружения TC_HOST_AVAILABLE_TIMEOUT_SECOND=значения таймаута в секундах, которая задает таймаут ожидания доступности с хостмашины соединения по tcp к портам контейнера
