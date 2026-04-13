#!/bin/sh

if [[ -z "$GID" ]]; then
	GID="$UID"
fi

BINARY_NAME=/usr/bin/mautrix-telegram

function fixperms {
	chown -R $UID:$GID /data

	# /opt/mautrix-telegram is read-only, so disable file logging if it's pointing there.
	if [[ "$(yq e '.logging.writers[1].filename' /data/config.yaml)" == "./logs/mautrix-telegram.log" ]]; then
		yq -I4 e -i 'del(.logging.writers[1])' /data/config.yaml
	fi
}

# Allow config/registration to be mounted outside /data (avoids subPath-in-PVC bug).
CONFIG_FILE=/data/config.yaml
REG_FILE=/data/registration.yaml
if [[ -f /etc/mautrix/config.yaml ]]; then
	CONFIG_FILE=/etc/mautrix/config.yaml
fi
if [[ -f /etc/mautrix/registration.yaml ]]; then
	REG_FILE=/etc/mautrix/registration.yaml
fi

if [[ ! -f $CONFIG_FILE ]]; then
	$BINARY_NAME -c $CONFIG_FILE -e
	echo "Didn't find a config file."
	echo "Copied default config file to $CONFIG_FILE"
	echo "Modify that config file to your liking."
	echo "Start the container again after that to generate the registration file."
	exit
fi

if [[ ! -f $REG_FILE ]]; then
	$BINARY_NAME -g -c $CONFIG_FILE -r $REG_FILE
	echo "Didn't find a registration file."
	echo "Generated one for you."
	echo "See https://docs.mau.fi/bridges/general/registering-appservices.html on how to use it."
	exit
fi

cd /data
fixperms
exec su-exec $UID:$GID $BINARY_NAME -c $CONFIG_FILE -r $REG_FILE
