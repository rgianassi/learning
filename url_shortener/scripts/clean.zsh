#!/usr/bin/zsh

SCRIPT_DIR=${0:a:h}

. "$SCRIPT_DIR"/vars.zsh

pushd "$ROOT_DIR"

rm -rf "$BUILD_DIR"

popd
