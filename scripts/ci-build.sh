#!/bin/sh -xe

if [ -z "${CI_TARGET}" ]; then
  echo "CI_TARGET is not set, refusing to build"
  exit 1
fi

echo "Building for $CI_OS-$CI_ARCH"

if [ "$CI_OS" = "linux" ]; then
  export PATH="$PATH:/usr/local/go/bin"
fi

go version

export CURRENT_BUILD_PATH=$(pwd)
export PATH="$PATH:$(go env GOPATH)/bin"

if [ "$CI_OS" = "windows" ]; then
  if [ "$CI_ARCH" = "386" ]; then
    export PATH="/mingw32/bin:$PATH"
  else
    export PATH="/mingw64/bin:$PATH"
  fi
fi

export CC="gcc"
export CXX="g++"
export WINDRES="windres"

export CI_VERSION="head"
export CI_BUILT_AT="$(date +%s)"
if [ -n "$CI_BUILD_TAG" ]; then
  export CI_VERSION="$CI_BUILD_TAG"
elif [ "master" != "$CI_BUILD_REF_NAME" ]; then
  export CI_VERSION="$CI_BUILD_REF_NAME"
fi

export CI_LDFLAGS="-X main.version=$CI_VERSION -X main.builtAt=$CI_BUILT_AT -X main.commit=$CI_BUILD_REF -X main.target=$CI_TARGET -w -s"

if [ "$CI_OS" = "windows" ]; then
  export CI_LDFLAGS="$CI_LDFLAGS -H windowsgui"
fi

TARGET=$CI_TARGET
if [ "$CI_OS" = "windows" ]; then
  TARGET=$TARGET.exe
fi

if [ "$CI_OS" = "windows" ]; then
  $WINDRES -o itch-setup.syso itch-setup.rc
  file itch-setup.syso
fi

if [ "$CI_OS" = "linux" ]; then
  export GO_TAGS="-tags gtk_3_14"
fi

if [ "$CI_OS" = "darwin" ]; then
  export CGO_CFLAGS=-mmacosx-version-min=10.10
  export CGO_LDFLAGS=-mmacosx-version-min=10.10
fi

export GOOS=$CI_OS
export GOARCH=$CI_ARCH
export CGO_ENABLED=1

# compile
go build -v -x -ldflags "$CI_LDFLAGS" $GO_TAGS -o $TARGET

file $TARGET

if [ "$CI_OS" = "windows" ]; then
  # sign *after* packing
  tools/signtool.exe sign //v //s MY //n "itch corp." //fd sha256 //tr http://timestamp.comodoca.com/?td=sha256 //td sha256 $TARGET
fi

if [ "$CI_OS" = "darwin" ]; then
  # sign *after* packing
  SIGNKEY="Developer ID Application: Amos Wenger (B2N6FSRTPV)"
  codesign --deep --force --verbose --sign "${SIGNKEY}" "${TARGET}"
  codesign --verify -vvvv "${TARGET}"
fi

BINARIES=broth/$CI_TARGET/$CI_OS-$CI_ARCH
mkdir -p $BINARIES
cp -rf $TARGET $BINARIES/

