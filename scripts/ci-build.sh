#!/bin/sh -xe

echo "Building for $CI_OS-$CI_ARCH"

if [ "$CI_OS" = "linux" ]; then
  export PATH="$PATH:/usr/local/go/bin"
fi

go version

export CURRENT_BUILD_PATH=$(pwd)
export GOPATH=$CURRENT_BUILD_PATH
export PATH="$PATH:$GOPATH/bin"
export CGO_ENABLED=1

# set up go cross-compile
go get github.com/mitchellh/gox

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

export CI_LDFLAGS="-X main.version=$CI_VERSION -X main.builtAt=$CI_BUILT_AT -X main.commit=$CI_BUILD_REF"

if [ "$CI_OS" = "windows" ]; then
  export CI_LDFLAGS="$CI_LDFLAGS -H windowsgui"
fi

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

if [ "$CI_OS" = "linux" ]; then
  export GOX_TAGS="-tags gtk_3_14"
fi

if [ "$CI_OS" = "windows" ]; then
  $WINDRES -o itchSetup.syso itchSetup.rc
  file itchSetup.syso
fi

# compile
gox -osarch "$CI_OS/$CI_ARCH" -ldflags "$CI_LDFLAGS" -cgo -output="itchSetup" $GOX_TAGS $PKG

file itchSetup.exe

EXT=""
if [ "$CI_OS" = "windows" ]; then
  EXT=".exe"
fi

BINARIES=binaries/$CI_OS-$CI_ARCH
mkdir -p $BINARIES
cp -rf itchSetup$EXT $BINARIES/

