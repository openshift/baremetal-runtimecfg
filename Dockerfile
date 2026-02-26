FROM registry.ci.openshift.org/openshift/release:golang-1.22 AS builder
WORKDIR /go/src/github.com/openshift/baremetal-runtimecfg
COPY . .
RUN mkdir build
RUN GO111MODULE=on go build --mod=vendor -o build ./cmd/...

FROM centos:stream9

RUN yum install -y diffutils dbus-tools && yum clean all

COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/build/* /usr/bin/
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/scripts/* /usr/bin/
COPY --from=builder /go/src/github.com/openshift/baremetal-runtimecfg/scripts/ip*tables /usr/sbin/

ENTRYPOINT ["/usr/bin/runtimecfg"]

LABEL io.k8s.display-name="baremetal-runtimecfg" \
      io.k8s.description="Retrieves Node and Cluster information for baremetal network config" \
      maintainer="Antoni Segura Puimedon <antoni@redhat.com>"
