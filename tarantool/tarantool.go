package tarantool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

const (
	DefaultTarantoolImage = "tarantool/tarantool:1.5"
	DefaultMasterPort     = "3301"
)

type Container struct {
	testcontainers.Container
	logger testcontainers.Logging
}

func (c *Container) Init(ctx context.Context) error {
	return c.ProduceLog(ctx)
}

// ProduceLog запускает процесс чтения логов контейнера с их выводом в testcontainers.Logging
func (c *Container) ProduceLog(ctx context.Context) error {
	if c.logger != nil {
		logConsumer := &LogConsumer{c.logger}
		c.FollowOutput(logConsumer)

		return c.StartLogProducer(ctx)
	}

	return nil
}

// ServerHostPort хост и порт сервера тарантула запущенного в контейнере
func (c *Container) ServerHostPort(ctx context.Context) (string, error) {
	return ServerHostPort(ctx, c.Container)
}

func ServerHostPort(ctx context.Context, c testcontainers.Container) (string, error) {
	containerPort, err := c.MappedPort(ctx, DefaultMasterPort+"/tcp")
	if err != nil {
		return "", err
	}

	host, err := c.Host(ctx)
	if err != nil {
		return "", err
	}

	addr := host + ":" + containerPort.Port()

	if timeoutSec, err := strconv.Atoi(os.Getenv("TC_HOST_AVAILABLE_TIMEOUT_SECOND")); err == nil {
		to := time.Duration(timeoutSec) * time.Second
		if err := checkAvailable(ctx, "tcp", addr, to); err != nil {
			return "", fmt.Errorf("host %s not available: %w", addr, err)
		}
	}

	return addr, nil
}

// checkAvailable проверяет возможность соединения с address
func checkAvailable(ctx context.Context, network, address string, waitTimeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, waitTimeout)

	g, ctx := errgroup.WithContext(ctx)

	defer cancel()

	defer func(startTime time.Time) {
		if time.Since(startTime) > time.Second {
			fmt.Printf("network connection to %s is very slow!\n", address)
		}
	}(time.Now())

	g.Go(func() error {
		c, err := net.Dial(network, address)
		defer func() {
			if c != nil {
				c.Close()
			}
		}()

		for errors.Is(err, unix.ECONNREFUSED) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				time.Sleep(50 * time.Millisecond)
				c, err = net.Dial(network, address)
			}
		}

		return nil
	})

	return g.Wait()
}

func (c *Container) StartContainer(ctx context.Context) error {
	defer func(c *Container, ctx context.Context) {
		_ = c.ProduceLog(ctx)
	}(c, ctx)

	return c.Start(ctx)
}

// RunContainer создает экземпляр контейнера тарантула
func RunContainer(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*Container, error) {
	c, err := NewContainer(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("can't create container: %w", err)
	}

	if err := c.StartContainer(ctx); err != nil {
		return nil, fmt.Errorf("can't start container: %w", err)
	}

	return c, nil
}

// NewContainer создает экземпляр контейнера тарантула
func NewContainer(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*Container, error) {
	req := testcontainers.ContainerRequest{
		Image:        DefaultTarantoolImage,
		ExposedPorts: []string{DefaultMasterPort + "/tcp"},
	}
	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          false,
	}

	for _, opt := range opts {
		opt.Customize(&genericContainerReq)
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	if err != nil {
		return nil, err
	}

	moduleContainer := &Container{
		Container: container,
		logger:    genericContainerReq.Logger,
	}

	if genericContainerReq.Started {
		if err := moduleContainer.Init(ctx); err != nil {
			return nil, err
		}
	}

	return moduleContainer, nil
}

func (c *Container) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}

	return c.Container.Start(ctx)
}

func (c *Container) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}

	timeout := 5 * time.Second

	return c.Container.Stop(ctx, &timeout)
}

func (c *Container) Terminate(ctx context.Context) error {
	return c.Container.Terminate(ctx)
}

// CopyDirToContainer копирует файлы (включая содержимое символических ссылок) расположенные в hostDirPath в контейнер в containerParentPath с правами fileMode
func (c *Container) CopyDirToContainer(ctx context.Context, hostDirPath string, containerParentPath string, fileMode int64) error {
	if err := c.Container.CopyDirToContainer(ctx, hostDirPath, containerParentPath, fileMode); err != nil {
		return fmt.Errorf("can't copy scripts files content from '%s' to container: %w", hostDirPath, err)
	}

	// копируем содержимое  символических ссылок, т.к. оригинальная реализация CopyDirToContainer в библиотеке игнорирует symlink
	if err := filepath.Walk(hostDirPath, func(file string, fi os.FileInfo, errFn error) error {
		if errFn != nil {
			return fmt.Errorf("error traversing the file system: %w", errFn)
		}

		if fi.Mode().Type() == os.ModeSymlink {
			relPath, err := filepath.Rel(hostDirPath, file)
			if err != nil {
				return err
			}

			containerPath := filepath.Join(containerParentPath, relPath)

			return c.Container.CopyFileToContainer(ctx, file, containerPath, fileMode)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("can't copy symlink files content to container: %w", err)
	}

	return nil
}

// WithTarantool15 конфигурирует запуск контейнера тарантула версии 1.5
func WithTarantool15(image string, waitTimeout time.Duration) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		req.Image = image
		req.WaitingFor = wait.ForLog("entering event loop").WithStartupTimeout(waitTimeout)
	}
}

// WithTarantool2 конфигурирует запуск контейнера тарантула версии 2
func WithTarantool2(image string, waitTimeout time.Duration) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		req.Image = image
		req.WaitingFor = wait.ForLog("entering the event loop").WithStartupTimeout(waitTimeout)
	}
}

func WithCopyFile(hostFilePath, containerFilePath string, fileMode int64) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		startup := testcontainers.ContainerLifecycleHooks{
			PreStarts: []testcontainers.ContainerHook{},
		}

		execFn := func(ctx context.Context, c testcontainers.Container) error {
			return c.CopyFileToContainer(ctx, hostFilePath, containerFilePath, fileMode)
		}

		startup.PreStarts = append(startup.PreStarts, execFn)

		req.LifecycleHooks = append(req.LifecycleHooks, startup)
	}
}

// WithConfigFile устанавливает конфиг тарантула из файла расположенного в cfgPath
func WithConfigFile(cfgPath string) testcontainers.CustomizeRequestOption {
	return WithCopyFile(cfgPath, "/etc/tarantool/tarantool.cfg", 0o755)
}

// WithConfigFileMapping устанавливает конфиг тарантула из файла расположенного в cfgPath в контейнер по пути containerCfgPath
func WithConfigFileMapping(cfgPath, containerCfgPath string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		cfgFile := testcontainers.ContainerFile{
			HostFilePath:      cfgPath,
			ContainerFilePath: containerCfgPath,
			FileMode:          0o755,
		}

		req.Files = append(req.Files, cfgFile)
	}
}

// WithScriptsMapping устанавливает папку со LUA скриптами из папки расположенной в scriptPath в папку контейнера containerScriptPath
func WithScriptsMapping(scriptPath string, containerScriptPath string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		scriptDir := testcontainers.ContainerMount{
			Source:   testcontainers.GenericBindMountSource{HostPath: scriptPath},
			Target:   testcontainers.ContainerMountTarget(containerScriptPath),
			ReadOnly: false,
		}

		req.Mounts = append(req.Mounts, scriptDir)
	}
}

// WithScripts копирует папку со LUA скриптами из папки расположенной в scriptPath в папку контейнера containerScriptPath
func WithScripts(scriptPath string, containerScriptPath string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		startup := testcontainers.ContainerLifecycleHooks{
			PreStarts: []testcontainers.ContainerHook{},
		}

		execFn := func(ctx context.Context, c testcontainers.Container) error {
			return c.CopyDirToContainer(ctx, scriptPath, containerScriptPath, 0o755)
		}

		startup.PreStarts = append(startup.PreStarts, execFn)

		req.LifecycleHooks = append(req.LifecycleHooks, startup)
	}
}

// WithLogger устанавливает вывод в logger
func WithLogger(logger testcontainers.Logging) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		req.Logger = logger
	}
}

// WithCommand запуск контейнера tarantool с пользовательской командой
func WithCommand(cmd []string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		if len(req.Cmd) == 0 {
			req.Cmd = []string{"sh", "-c"}
		}

		req.Cmd = append(req.Cmd, cmd...)
	}
}

// WithArguments запуск контейнера tarantool с аргументами для entrypoint
func WithArguments(args ...string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		req.Cmd = append(req.Cmd, args...)
	}
}

// WithName устанавливает возможность переиспользовать контейнер
func WithName(name string, reuse bool) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		req.Reuse = reuse
		req.Name = name
	}
}

// WithEnv копирует переменные окружения начинающиеся с prefix в контейнер
func WithEnv(prefix string) testcontainers.CustomizeRequestOption {
	env := map[string]string{}

	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, prefix) {
			continue
		}

		tokens := strings.Split(v, "=")
		env[tokens[0]] = tokens[1]
	}

	return func(o *testcontainers.GenericContainerRequest) {
		o.Env = env
	}
}

// WithHookOnStopTarantool регистрирует функции выполняющиеся перед остановкой контейнера
func WithHookOnStopTarantool(hook func(ctx context.Context, connection string) error) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		startup := testcontainers.ContainerLifecycleHooks{
			PreStops: []testcontainers.ContainerHook{},
		}

		execFn := func(ctx context.Context, c testcontainers.Container) error {
			connStr, err := ServerHostPort(ctx, c)
			if err != nil {
				return fmt.Errorf("can't get host:port of tarantool container")
			}

			return hook(ctx, connStr)
		}

		startup.PreStops = append(startup.PreStops, execFn)

		req.LifecycleHooks = append(req.LifecycleHooks, startup)
	}
}

// WithHookOnStartTarantool регистрирует функции выполняющиеся после старта контейнера и получения соединения к БД
func WithHookOnStartTarantool(hook func(ctx context.Context, connection string) error) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) {
		startup := testcontainers.ContainerLifecycleHooks{
			PostStarts: []testcontainers.ContainerHook{},
		}

		execFn := func(ctx context.Context, c testcontainers.Container) error {
			connStr, err := ServerHostPort(ctx, c)
			if err != nil {
				return fmt.Errorf("can't get host:port of tarantool container")
			}

			return hook(ctx, connStr)
		}

		startup.PostStarts = append(startup.PostStarts, execFn)

		req.LifecycleHooks = append(req.LifecycleHooks, startup)
	}
}

// LogConsumer реализует обработку полученных логов контейнера
type LogConsumer struct {
	testcontainers.Logging
}

func (lc *LogConsumer) Accept(l testcontainers.Log) {
	if lc.Logging != nil {
		lc.Logging.Printf(string(l.Content))
	}
}
