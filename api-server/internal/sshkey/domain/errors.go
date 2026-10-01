package domain

import "errors"

var (
	// ErrKeyNotFound means no SSH Key has the requested id, or it belongs to another User. The
	// two are deliberately indistinguishable so a caller cannot probe other Users' keys.
	ErrKeyNotFound = errors.New("ssh key not found")
	// ErrDeploymentKeyNotFound means the installation has no Deployment Key yet. The installation
	// step (`swallow-api deployment-key ensure`) creates it, so outside of installation this means
	// that step was skipped; OS and Platform deployment are refused until it runs.
	ErrDeploymentKeyNotFound = errors.New("deployment key not found")
	// ErrDeploymentKeyExists means a Deployment Key already exists; there is exactly one per
	// installation. Bootstrap treats it as "another process created it first".
	ErrDeploymentKeyExists = errors.New("deployment key already exists")
	// ErrDuplicateKey means another SSH Key already has this key material (fingerprint).
	ErrDuplicateKey = errors.New("ssh key already exists")
	// ErrDuplicateName means the owner already has an Access Key with this name.
	ErrDuplicateName = errors.New("ssh key name already exists")
	// ErrInvalidName means a key name is empty or longer than MaxNameLength.
	ErrInvalidName = errors.New("invalid ssh key name")
	// ErrInvalidKey means the key material could not be parsed.
	ErrInvalidKey = errors.New("invalid ssh key")
	// ErrUnsupportedKey means the key parsed but its algorithm or strength is not accepted.
	ErrUnsupportedKey = errors.New("unsupported ssh key type")
	// ErrPassphraseProtected means a private key is encrypted. swallow uses the Deployment Key
	// unattended, so it cannot hold a key that needs a passphrase.
	ErrPassphraseProtected = errors.New("ssh private key is passphrase protected")
	// ErrDeploymentKeyImmutable means a request tried to delete the Deployment Key; it can only
	// be replaced or regenerated.
	ErrDeploymentKeyImmutable = errors.New("the deployment key cannot be deleted")
	// ErrDeploymentKeyNotRegistered means a key-capable provisioner could not be made to hold the
	// Deployment Key, so a Server it deploys now would not authorize swallow.
	ErrDeploymentKeyNotRegistered = errors.New("deployment key is not registered in the provisioner")
)
