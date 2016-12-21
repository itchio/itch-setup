#!/bin/sh -xe

echo "Building for darwin-$CI_ARCH"

go version

export CURRENT_BUILD_PATH=$(pwd)
export GOPATH=$CURRENT_BUILD_PATH
export PATH="$PATH:$GOPATH/bin"
export CGO_ENABLED=1

# set up go cross-compile
go get github.com/mitchellh/gox

export CC="${TRIPLET}gcc"
export CXX="${TRIPLET}g++"

export CI_VERSION="head"
export CI_BUILT_AT="$(date +%s)"
if [ -n "$CI_BUILD_TAG" ]; then
  export CI_VERSION="$CI_BUILD_TAG"
elif [ "master" != "$CI_BUILD_REF_NAME" ]; then
  export CI_VERSION="$CI_BUILD_REF_NAME"
fi

export CI_LDFLAGS="-X main.version=$CI_VERSION -X main.builtAt=$CI_BUILT_AT -X main.commit=$CI_BUILD_REF"

TARGET=butler
if [ "$CI_OS" = "windows" ]; then
  TARGET=$TARGET.exe
else
  export PATH=$PATH:/usr/local/go/bin
fi

export PKG=github.com/itchio/butler

mkdir -p src/$PKG

# rsync will complain about vanishing files sometimes, who knows where they come from
rsync -a --exclude 'src' . src/$PKG || echo "rsync complained (code $?)"

# grab deps
GOOS=$CI_OS GOARCH=$CI_ARCH go get -v -d -t $PKG

# compile
gox -osarch "$CI_OS/$CI_ARCH" -ldflags "$CI_LDFLAGS" -cgo -output="itchSetup" $PKG

