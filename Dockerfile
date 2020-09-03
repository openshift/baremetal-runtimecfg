FROM registry.svc.ci.openshift.org/openshift/release:golang-1.13 AS builder
WORKDIR /go/src/github.com/openshift/baremetal-runtimecfg
COPY . .
RUN GO111MODULE=on go build --mod=vendor -o runtimecfg ./cmd/runtimecfg
RUN GO111MODULE=on go build --mod=vendor cmd/dynkeepalived/dynkeepalived.go
RUN GO111MODULE=on go build --mod=vendor cmd/corednsmonitor/corednsmonitor.go
RUN GO111MODULE=on go build --mod=vendor cmd/monitor/monitor.go
RUN GO111MODULE=on go build --mod=vendor cmd/unicastipserver/unicastipserver.go

FROM centos:8

RUN yum install -y dhcp-client diffutils && yum clean all

COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/runtimecfg /usr/bin/
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/monitor /usr/bin
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/dynkeepalived /usr/bin
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/corednsmonitor /usr/bin
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/unicastipserver /usr/bin
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/scripts/* /usr/bin/
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/scripts/ip*tables /usr/sbin/

ENTRYPOINT ["/usr/bin/runtimecfg"]

LABEL io.k8s.display-name="baremetal-runtimecfg" \
      io.k8s.description="Retrieves Node and Cluster information for baremetal network config" \
      maintainer="Antoni Segura Puimedon <antoni@redhat.com>"
