# Settings of the installation's Pulp, the Site's Local Repository. Pulp loads this file as Python;
# the secrets come from the Compose secrets under /run/secrets, the origin from the environment.
import os


def _secret(name):
    with open(os.path.join("/run/secrets", name), encoding="utf-8") as secret:
        return secret.read().strip()


SECRET_KEY = _secret("pulp_secret_key")

# The address deployed Servers reach this Pulp at (SWALLOW_ADDRESS and SWALLOW_PULP_PORT). Registry
# blob redirects, the registry token realm, and the package links of the Python index are absolute
# URLs on it.
CONTENT_ORIGIN = os.environ["SWALLOW_PULP_ORIGIN"].rstrip("/")

# Role and database `pulp` in the installation's shared PostgreSQL, created by swallowctl.
DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.postgresql",
        "HOST": "postgres",
        "PORT": "5432",
        "NAME": "pulp",
        "USER": "pulp",
        "PASSWORD": _secret("pulp_db_password"),
        "CONN_MAX_AGE": 0,
    }
}

ANALYTICS = False

TOKEN_SERVER = CONTENT_ORIGIN + "/token/"
TOKEN_AUTH_DISABLED = False
TOKEN_SIGNATURE_ALGORITHM = "ES256"
PUBLIC_KEY_PATH = "/etc/pulp/certs/container_auth_public_key.pem"
PRIVATE_KEY_PATH = "/etc/pulp/certs/container_auth_private_key.pem"

PYPI_API_HOSTNAME = CONTENT_ORIGIN
