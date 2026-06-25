FROM registry.access.redhat.com/ubi9/go-toolset:1.26 AS builder

WORKDIR /opt/app-root/src
ENV CGO_ENABLED=0

# Copy all sources, including go.mod and go.sum, at once
COPY --chown=1001:0 . .

# Konflux will automatically inject `. /cachi2/cachi2.env &&` before this RUN 
# to point the Go toolchain to the offline dependency cache it prefetched.
RUN LDFLAGS="-s -w" make build

## Final image

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:5b74fce9d6e629942a0c6dc0f546c193e70d7f974d999a48c948c53dd3d36362

LABEL \
  name="fbc-update-planner" \
  com.redhat.component="fbc-update-planner" \
  description="Fetches operator lifecycle data from the Red Hat Product Life Cycle Center (PLCC) API, validates and filters it, and converts it into File-Based Catalog (FBC) blobs" \
  io.k8s.description="Fetches operator lifecycle data from the Red Hat Product Life Cycle Center (PLCC) API, validates and filters it, and converts it into File-Based Catalog (FBC) blobs" \
  io.k8s.display-name="fbc-update-planner" \
  summary="FBC update planner CLI" \
  io.openshift.tags="konflux,operator,olm,fbc"

COPY --from=builder /opt/app-root/src/bin/plcc2fbc /usr/local/bin/plcc2fbc

COPY LICENSE /licenses/LICENSE

# OpenShift preflight and Tekton task compatibility
USER 1001

ENTRYPOINT ["/usr/local/bin/plcc2fbc"]
