// Package usecase is the resources and their verbs. Each verb checks its
// arguments, describes one request, sends it through a port.Port, and hands
// the answer to a codec decoder — it never reads Dify's JSON itself.
package usecase
