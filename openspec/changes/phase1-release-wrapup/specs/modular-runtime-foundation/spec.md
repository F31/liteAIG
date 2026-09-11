# modular-runtime-foundation Specification (Delta)

## MODIFIED Requirements

### Requirement: Single-binary modular runtime
LiteAIG SHALL build as one `liteaig` binary by default and SHALL support `all`, `gateway`, and `control` runtime modes without introducing internal HTTP or gRPC calls between modules in `all` mode. Each runtime mode SHALL start its mode-specific servers through a lifecycle that performs graceful drain and shutdown, so that terminating processes report not-ready, finish in-flight requests, protect long streams, finalize accounting/budget/lease, and exit within configured timeouts as specified by the graceful-drain-lifecycle capability.

#### Scenario: Combined mode uses in-process boundaries
- **WHEN** LiteAIG starts with `mode=all`
- **THEN** Control Plane and Data Plane capabilities run in one process through Go contracts
- **AND** no loopback network service is required for their internal interaction

#### Scenario: Runtime mode shuts down via drain
- **WHEN** a `gateway` or `all` process is signalled to terminate
- **THEN** its HTTP servers follow the drain sequence (not-ready, finish in-flight, protect streams, finalize, exit)
- **AND** a `control`-only process also drains its admin API without terminating active operations abruptly
