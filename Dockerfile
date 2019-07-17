FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12 AS builder
WORKDIR /go/src/github.com/openshift/baremetal-runtimecfg
COPY . .
RUN GO111MODULE=on go build --mod=vendor cmd/runtimecfg/runtimecfg.go
RUN GO111MODULE=on go build --mod=vendor cmd/monitor/monitor.go

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/runtimecfg /usr/bin/
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/monitor /usr/bin
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/scripts/iptables /usr/bin

ENTRYPOINT ["/usr/bin/runtimecfg"]

LABEL io.k8s.display-name="baremetal-runtimecfg" \
      io.k8s.description="Retrieves Node and Cluster information for baremetal network config" \
      maintainer="Antoni Segura Puimedon <antoni@redhat.com>"
