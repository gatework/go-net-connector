// Package fleet turns an inventory into a run plan. Rules map inventory
// identities to registered connector platforms; a Plan gives every inventory
// row a status (ready, filtered, unsupported vendor, duplicate host, sample
// limit, ...) and one Task per ready device, with its transfer target in
// TFTP mode.
package fleet
