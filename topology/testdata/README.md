# Topology regression fixtures

These are synthetic records derived from the frozen Ruby reference, not captures
from production equipment. Model, firmware, and collection date are unknown.
The fixture strings deliberately contain only synthetic peer names and addresses.

`neighbors.json` covers H3C's two column orders (wireless shares the H3C dialect),
IOS/NX-OS CDP, Huawei spaced interfaces, Hillstone count evidence, Fortinet LLDP,
Junos LLDP, and PAN-OS local/neighbor blocks. Source:

- `.reference/net-connector/test/topology_test.rb`
- `.reference/net-connector/test/vendor_topology_parity_test.rb`
- `.reference/net-connector/test/topology_strategy_test.rb`
- `.reference/net-connector/test/support/topology_fixture.rb`

`topology_test.go` drives full connector sessions through `net.Pipe`, including
exact commands, configuration view transitions, readback, and separate save
completion evidence. Its save completion strings follow the frozen
`test/support/topology_fixture.rb` and provenance documented in
`test/fixtures/topology/README.md`. Hillstone completion evidence remains the
reference's conservative inference; it is not firmware compatibility evidence.

The test suite requires neither Ruby nor `.reference` at runtime. It proves
local parsing and dialogue behavior, not physical persistence, production device
compatibility, or protection against changes by another administrator after the
last revalidation/readback.
