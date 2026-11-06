# Settings of the Pulp behind the dev Site's Local Repository (decision 078). Pulp loads this file as Python;
# the values that differ per installation come from the environment of the compose services.
import os

SECRET_KEY = os.environ["SWALLOW_PULP_SECRET_KEY"]

# The address deployed Servers reach this Pulp at. Registry blob redirects, the registry token
# realm, and the package links of the Python index are absolute URLs on it.
CONTENT_ORIGIN = os.environ["SWALLOW_PULP_ORIGIN"].rstrip("/")

DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.postgresql",
        "HOST": os.environ["SWALLOW_PULP_DB_HOST"],
        "PORT": os.environ.get("SWALLOW_PULP_DB_PORT", "5432"),
        "NAME": "pulp",
        "USER": "pulp",
        "PASSWORD": os.environ["SWALLOW_PULP_DB_PASSWORD"],
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
