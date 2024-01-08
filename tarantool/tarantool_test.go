package tarantool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lomik/go-tnt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tarantool2 "github.com/viciious/go-tarantool"
)

func TestReuseContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	tests := []struct {
		name  string
		opts  []testcontainers.ContainerCustomizer
		reuse bool
	}{
		{
			name: "Tarantool1.5 not reused",
			opts: []testcontainers.ContainerCustomizer{
				WithName("tarantool15", false),
				WithTarantool15("tarantool/tarantool:1.5", 5*time.Second),
				WithConfigFile("testdata/tarantool.cfg"),
			},
		},
		{
			name: "Tarantool1.5 reused",
			opts: []testcontainers.ContainerCustomizer{
				WithName("tarantool15", true),
				WithTarantool15("tarantool/tarantool:1.5", 5*time.Second),
				WithConfigFile("testdata/tarantool.cfg"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container, err := RunContainer(ctx, tt.opts...)
			require.NoError(t, err)

			err = container.Stop(ctx)
			require.NoError(t, err)

			container2, err := RunContainer(ctx, tt.opts...)

			if tt.reuse {
				require.Equal(t, container.GetContainerID(), container2.GetContainerID())
			} else {
				require.Error(t, err)
			}

			// Clean up the container after the test is complete
			t.Cleanup(func() {
				if err := container.Terminate(ctx); err != nil {
					t.Fatalf("failed to terminate container: %s", err)
				}

				if container2 == nil {
					return
				}

				if err := container2.Terminate(ctx); err != nil {
					t.Fatalf("failed to terminate container: %s", err)
				}
			})
		})
	}
}

func TestRunContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	tests := []struct {
		name     string
		opts     []testcontainers.ContainerCustomizer
		env      map[string]string
		checkCon func(*testing.T, string) bool
	}{
		{
			name: "Tarantool1.5",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool15("tarantool/tarantool:1.5", 5*time.Second),
				WithConfigFile("testdata/tarantool.cfg"),
				WithHookOnStartTarantool(func(ctx context.Context, connection string) error {
					checkConnection15(t, connection)

					return nil
				}),
			},
			checkCon: checkConnection15,
		},
		{
			name: "Tarantool luabank",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool15("ebirukov/luabank", 5*time.Second),
				WithConfigFileMapping("testdata/tarantool.cfg", "/etc/tarantool.cfg"),
				WithCommand([]string{"tarantool_box --init_storage && tarantool_box"}),
				WithLogger(testcontainers.Logger),
			},
			checkCon: checkConnection15,
		},
		{
			name: "Tarantool custom registry",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool15("ebirukov/cloud-c7-test-tnt15", 5*time.Second),
				WithConfigFile("testdata/tarantool.cfg"),
				WithLogger(testcontainers.Logger),
			},
			checkCon: checkConnection15,
		},
		{
			name: "Tarantool 2",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool2("tarantool/tarantool:2.10", 5*time.Second),
				WithHookOnStopTarantool(func(ctx context.Context, connection string) error {
					checkConnection2(t, connection)

					return nil
				}),
			},
		},
		{
			name: "Tarantool 2 with init script",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool2("tarantool/tarantool:2.10", 5*time.Second),
				WithConfigFileMapping(resolveTestDataPath("testdata/tarantool2_script.lua"), "/opt/tarantool/init.lua"),
				WithArguments("/opt/tarantool/init.lua"),
			},
			checkCon: checkConnection2,
		},
		{
			name: "Tarantool 2 with init script",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool2("tarantool/tarantool:2.10", 5*time.Second),
				WithConfigFileMapping(resolveTestDataPath("testdata/tarantool2_script.lua"), "/opt/tarantool/init.lua"),
				WithArguments("/opt/tarantool/init.lua"),
			},
			checkCon: checkAccessToLuaFunc,
		},
		{
			name: "Tarantool 2 with scripts",
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool2("tarantool/tarantool:2.11", 5*time.Second),
				WithScripts(resolveTestDataPath("testdata"), "/opt/tarantool/"),
				WithArguments("/opt/tarantool/testdata/tarantool2_script.lua"),
			},
			checkCon: checkAccessToLuaFunc,
		},
		{
			name: "Tarantool 2.11 with env init script",
			env: map[string]string{
				// https://www.tarantool.io/ru/doc/latest/book/admin/instance_config/#preloading-lua-scripts-and-modules
				"TT_PRELOAD": "/opt/tarantool/init.lua",
			},
			opts: []testcontainers.ContainerCustomizer{
				WithTarantool2("tarantool/tarantool:2.11", 5*time.Second),
				WithConfigFileMapping(resolveTestDataPath("testdata/tarantool_2.11_init.lua"), "/opt/tarantool/init.lua"),
			},
			checkCon: checkAccessToLuaFunc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// pass env vars to container
			for k, v := range tt.env {
				require.NoError(t, os.Setenv(k, v))
				tt.opts = append(tt.opts, WithEnv(k))
			}

			container, err := RunContainer(ctx, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}

			// Clean up the container after the test is complete
			t.Cleanup(func() {
				if err = container.Terminate(ctx); err != nil {
					t.Fatalf("failed to terminate container: %s", err)
				}
			})

			connStr, err := container.ServerHostPort(ctx)
			assert.NoError(t, err)

			id, err := container.MappedPort(ctx, "3301/tcp")
			assert.NoError(t, err)

			host, err := container.Host(ctx)
			assert.NoError(t, err)

			assert.Equal(t, fmt.Sprintf("%s:%s", host, id.Port()), connStr)

			if tt.checkCon != nil {
				assert.True(t, tt.checkCon(t, connStr), "can't connect to tarantool with %s", connStr)
			}
		})
	}
}

func resolveTestDataPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	_, filename, _, _ := runtime.Caller(0)
	curDir := filepath.Dir(filename)

	return filepath.Join(curDir, path)
}

func checkAccessToLuaFunc(t *testing.T, connStr string) bool {
	conn, err := tarantool2.Connect(connStr, &tarantool2.Options{})
	if err != nil {
		t.Fatal(err)
	}

	query := &tarantool2.Call{Name: "todo.info"}
	resp := conn.Exec(context.Background(), query)
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}

	return assert.Equal(t, [][]interface{}{{"info"}}, resp.Data)
}

func checkConnection2(t *testing.T, connStr string) bool {
	opts := tarantool2.Options{}
	conn, err := tarantool2.Connect(connStr, &opts)
	if err != nil {
		t.Fatal(err)
	}

	query := &tarantool2.Call{Name: "dostring", Tuple: []interface{}{"return box.info.status"}}
	resp := conn.Exec(context.Background(), query)
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}

	return assert.Equal(t, [][]interface{}{{"running"}}, resp.Data)
}

func checkConnection15(t *testing.T, connStr string) bool {
	c, err := tnt.Connect(connStr, &tnt.Options{})
	if err != nil {
		t.Fatal(err)
	}

	boxInfoStatus := &tnt.Call{
		Name:  tnt.Bytes("box.dostring"),
		Tuple: tnt.Tuple{tnt.Bytes("return box.info.status")},
	}

	tuples, err := c.Execute(boxInfoStatus)
	if err != nil {
		t.Fatal(err)
	}

	return assert.EqualValues(t, tnt.Tuple{tnt.Bytes("primary")}, tuples[0])
}
