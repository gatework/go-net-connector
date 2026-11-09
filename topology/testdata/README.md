# Topology regression fixtures

Synthetic neighbor records, not captures from production equipment. Model,
firmware and collection date are unknown; peer names and addresses are synthetic.

`neighbors.json` covers H3C's two column orders (wireless shares the H3C dialect),
IOS/NX-OS CDP, Huawei spaced interfaces, Hillstone count evidence, Fortinet LLDP,
Junos LLDP, and PAN-OS local/neighbor blocks.

`topology_test.go` drives full connector sessions through `net.Pipe`, including
exact commands, configuration view transitions, readback, and separate save
completion evidence. Hillstone completion evidence is a conservative inference,
not firmware compatibility evidence.

These tests prove local parsing and dialogue behavior, not physical persistence,
production device compatibility, or protection against changes by another
administrator after the last revalidation/readback.
