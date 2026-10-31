// Package genfsimg builds Boot ISOs with iPXE's genfsimg and grub-mkstandalone (decisions 049 and
// 057): it packages the prebuilt iPXE and GRUB binaries the API image ships with a rendered boot
// script, and needs no compiler at runtime.
//
// It owns only the files under its directory; Boot Media policy, the Boot ISO records, and when a
// rebuild is due belong to the core provisioning and server contexts.
package genfsimg
