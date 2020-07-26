# runtimecfg

runtimecfg is small utitily that reads KubeConfig and checks the current system
for rendering OpenShift baremetal networking configuration.

## Usage

    runtimecfg [command]

The available commands are:
* display: Displays the struct that contains the information for rendering     
* help: Help about any command
* render: Renders go templates with the runtime configuration. Takes a
  -o/--out-dir parameter to specify where to write the rendered files.

The available flags are:
* --api-vip: Virtual IP Address to reach the OpenShift API
* --dns-vip: Virtual IP Address to reach an OpenShift node resolving DNS server
* --ingress-vip Virtual IP Address to reach the OpenShift Ingress Routers

Note that you must pass at least one VIP for the VRRP interface to be found.

## Test

In order to run the tests you should have all these prerequisites:
* make
* docker
* docker-compose

```bash
make docker_test
```