package main

import (
	"net/netip"
	"testing"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

type testInspectOpts struct {
	name         string
	image        string
	labels       map[string]string
	cmd          []string
	entrypoint   []string
	env          []string
	portBindings network.PortMap
	mounts       []container.MountPoint
	networks     map[string]*network.EndpointSettings
	restart      container.RestartPolicyMode
}

func newTestInspect(opts testInspectOpts) container.InspectResponse {
	return container.InspectResponse{
		Name: opts.name,
		Config: &container.Config{
			Image:      opts.image,
			Labels:     opts.labels,
			Cmd:        opts.cmd,
			Entrypoint: opts.entrypoint,
			Env:        opts.env,
		},
		HostConfig: &container.HostConfig{
			PortBindings:  opts.portBindings,
			RestartPolicy: container.RestartPolicy{Name: opts.restart},
		},
		Mounts: opts.mounts,
		NetworkSettings: &container.NetworkSettings{
			Networks: opts.networks,
		},
	}
}

func newTestImage(cmd, entrypoint, env []string) image.InspectResponse {
	return image.InspectResponse{
		Config: &dockerspec.DockerOCIImageConfig{
			ImageConfig: ocispec.ImageConfig{
				Cmd:        cmd,
				Entrypoint: entrypoint,
				Env:        env,
			},
		},
	}
}

type composeTestCase struct {
	name       string
	containers []container.InspectResponse
	images     map[string]image.InspectResponse
	project    string
	want       composeFile
	wantYAML   string
}

func composeTestCases() []composeTestCase {
	return []composeTestCase{
		{
			name: "stack de dos servicios con depends_on",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/myapp_db_1",
					image:  "postgres:16",
					labels: map[string]string{"com.docker.compose.service": "db"},
				}),
				newTestInspect(testInspectOpts{
					name:  "/myapp_web_1",
					image: "nginx:latest",
					labels: map[string]string{
						"com.docker.compose.service":    "web",
						"com.docker.compose.depends_on": "db:service_started:true",
					},
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "myapp",
			want: composeFile{
				services: []composeService{
					{name: "db", image: "postgres:16"},
					{name: "web", image: "nginx:latest", dependsOn: []string{"db"}},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  db:\n" +
				"    image: postgres:16\n" +
				"  web:\n" +
				"    image: nginx:latest\n" +
				"    depends_on:\n" +
				"      - db\n",
		},
		{
			name: "puerto udp",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/proj_dns_1",
					image:  "dnsmasq",
					labels: map[string]string{"com.docker.compose.service": "dns"},
					portBindings: network.PortMap{
						network.MustParsePort("53/udp"): {{HostPort: "53"}},
					},
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{name: "dns", image: "dnsmasq", ports: []string{"53:53/udp"}},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  dns:\n" +
				"    image: dnsmasq\n" +
				"    ports:\n" +
				"      - 53:53/udp\n",
		},
		{
			name: "bind :ro",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/proj_cache_1",
					image:  "redis",
					labels: map[string]string{"com.docker.compose.service": "cache"},
					mounts: []container.MountPoint{
						{Type: mount.TypeBind, Source: "/host/data", Destination: "/data", RW: false},
					},
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{name: "cache", image: "redis", volumes: []string{"/host/data:/data:ro"}},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  cache:\n" +
				"    image: redis\n" +
				"    volumes:\n" +
				"      - /host/data:/data:ro\n",
		},
		{
			name: "env filtrado contra la imagen",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/proj_app_1",
					image:  "myimg",
					labels: map[string]string{"com.docker.compose.service": "app"},
					env:    []string{"PATH=/usr/bin", "FOO=bar", "BAZ=qux"},
				}),
			},
			images: map[string]image.InspectResponse{
				"myimg": newTestImage(nil, nil, []string{"PATH=/usr/bin", "BAZ=qux"}),
			},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{name: "app", image: "myimg", environment: []string{"FOO=bar"}},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  app:\n" +
				"    image: myimg\n" +
				"    environment:\n" +
				"      - FOO=bar\n",
		},
		{
			name: "restart y volumen/red con prefijo de proyecto",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/myapp_worker_1",
					image:  "worker:latest",
					labels: map[string]string{"com.docker.compose.service": "worker"},
					mounts: []container.MountPoint{
						{Type: mount.TypeVolume, Name: "myapp_data", Destination: "/data", RW: true},
					},
					networks: map[string]*network.EndpointSettings{
						"myapp_default": {},
						"myapp_backend": {},
					},
					restart: container.RestartPolicyAlways,
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "myapp",
			want: composeFile{
				services: []composeService{
					{
						name:     "worker",
						image:    "worker:latest",
						restart:  "always",
						volumes:  []string{"data:/data"},
						networks: []string{"backend", "default"},
					},
				},
				volumes:  map[string]string{"data": "myapp_data"},
				networks: map[string]string{"backend": "myapp_backend"},
			},
			wantYAML: "services:\n" +
				"  worker:\n" +
				"    image: worker:latest\n" +
				"    volumes:\n" +
				"      - data:/data\n" +
				"    networks:\n" +
				"      - backend\n" +
				"      - default\n" +
				"    restart: always\n" +
				"volumes:\n" +
				"  data:\n" +
				"    name: myapp_data\n" +
				"networks:\n" +
				"  backend:\n" +
				"    name: myapp_backend\n",
		},
		{
			name: "container minimo (solo image)",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:  "/minimal",
					image: "alpine",
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{name: "minimal", image: "alpine"},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  minimal:\n" +
				"    image: alpine\n",
		},
		{
			name: "container sin proyecto (project vacio, red bridge excluida)",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/standalone",
					image:  "busybox",
					labels: map[string]string{"com.docker.compose.service": "standalone"},
					networks: map[string]*network.EndpointSettings{
						"bridge":  {},
						"app_net": {},
					},
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "",
			want: composeFile{
				services: []composeService{
					{name: "standalone", image: "busybox", networks: []string{"app_net"}},
				},
				volumes:  map[string]string{},
				networks: map[string]string{"app_net": "app_net"},
			},
			wantYAML: "services:\n" +
				"  standalone:\n" +
				"    image: busybox\n" +
				"    networks:\n" +
				"      - app_net\n" +
				"networks:\n" +
				"  app_net: {}\n",
		},
		{
			name: "command y entrypoint personalizados frente a la imagen",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:       "/proj_runner_1",
					image:      "runner:latest",
					labels:     map[string]string{"com.docker.compose.service": "runner"},
					cmd:        []string{"python", "app.py"},
					entrypoint: []string{"/bin/sh", "-c"},
				}),
			},
			images: map[string]image.InspectResponse{
				"runner:latest": newTestImage([]string{"oldcmd"}, []string{"/bin/oldentry"}, nil),
			},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{
						name:       "runner",
						image:      "runner:latest",
						command:    []string{"python", "app.py"},
						entrypoint: []string{"/bin/sh", "-c"},
					},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  runner:\n" +
				"    image: runner:latest\n" +
				"    command: [\"python\", \"app.py\"]\n" +
				"    entrypoint: [\"/bin/sh\", \"-c\"]\n",
		},
		{
			name: "puertos multiples con HostIP",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/proj_api_1",
					image:  "api",
					labels: map[string]string{"com.docker.compose.service": "api"},
					portBindings: network.PortMap{
						network.MustParsePort("22/tcp"): {{HostPort: "2222"}},
						network.MustParsePort("80/tcp"): {{HostPort: "8080", HostIP: netip.MustParseAddr("127.0.0.1")}},
					},
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{name: "api", image: "api", ports: []string{"2222:22", "127.0.0.1:8080:80"}},
				},
				volumes:  map[string]string{},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  api:\n" +
				"    image: api\n" +
				"    ports:\n" +
				"      - 2222:22\n" +
				"      - 127.0.0.1:8080:80\n",
		},
		{
			name: "volumen nombrado de solo lectura",
			containers: []container.InspectResponse{
				newTestInspect(testInspectOpts{
					name:   "/proj_cache_1",
					image:  "redis",
					labels: map[string]string{"com.docker.compose.service": "cache"},
					mounts: []container.MountPoint{
						{Type: mount.TypeVolume, Name: "proj_cachevol", Destination: "/var/cache", RW: false},
					},
				}),
			},
			images:  map[string]image.InspectResponse{},
			project: "proj",
			want: composeFile{
				services: []composeService{
					{name: "cache", image: "redis", volumes: []string{"cachevol:/var/cache:ro"}},
				},
				volumes:  map[string]string{"cachevol": "proj_cachevol"},
				networks: map[string]string{},
			},
			wantYAML: "services:\n" +
				"  cache:\n" +
				"    image: redis\n" +
				"    volumes:\n" +
				"      - cachevol:/var/cache:ro\n" +
				"volumes:\n" +
				"  cachevol:\n" +
				"    name: proj_cachevol\n",
		},
	}
}

func TestNewComposeFile(t *testing.T) {
	t.Parallel()

	for _, tc := range composeTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newComposeFile(tc.containers, tc.images, tc.project)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestComposeFileRender(t *testing.T) {
	t.Parallel()

	for _, tc := range composeTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.want.render()

			require.Equal(t, tc.wantYAML, got)
		})
	}
}

func TestComposeNilConfig(t *testing.T) {
	t.Parallel()

	t.Run("composePorts con hostConfig nil", func(t *testing.T) {
		t.Parallel()

		got := composePorts(nil)

		require.Nil(t, got)
	})

	t.Run("composeNetworks con networkSettings nil", func(t *testing.T) {
		t.Parallel()

		got := composeNetworks(nil, "proj", map[string]string{})

		require.Nil(t, got)
	})

	t.Run("composeRestart con hostConfig nil", func(t *testing.T) {
		t.Parallel()

		got := composeRestart(nil)

		require.Empty(t, got)
	})
}
