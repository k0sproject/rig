package hostkey

// LookupHostFunc exposes the package-level lookupHost for override in tests.
var LookupHostFunc = &lookupHost

// LookupHostMu exposes the mutex that guards lookupHost.
var LookupHostMu = &lookupHostMu
