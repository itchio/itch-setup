UPX_LEVEL ?= -1

ifeq ($(OS),Windows_NT)
SETUP_OS:=windows
  ifeq (GOOS,386)
GOBIN:=${GOPATH}/bin/windows_386
  else
GOBIN:=${GOPATH}/bin
  endif
else
UNAME_S := $(shell uname -s)
  ifeq ($(UNAME_S),Linux)
SETUP_OS:=linux
GOBIN:=${GOPATH}/bin
  else
  ifeq ($(UNAME_S),Darwin)
SETUP_OS:=darwin
GOBIN:=${GOPATH}/bin
  else
SETUP_OS:=unknown
  endif
endif
endif

all:
	@make $(SETUP_OS)

linux:
	go get -v -x -tags gtk_3_18
	upx ${UPX_LEVEL} ${GOBIN}/itch-setup
	cp -f ${GOBIN}/itch-setup ${GOBIN}/kitch-setup

windows:
	windres -o itch-setup.syso itch-setup.rc
	go get -v -x -ldflags="-H windowsgui"
	upx ${UPX_LEVEL} ${GOBIN}/itch-setup.exe
	cp -f ${GOBIN}/itch-setup.exe ${GOBIN}/kitch-setup.exe

darwin:
	go get -v -x
	upx ${UPX_LEVEL} ${GOBIN}/itch-setup
	cp -f ${GOBIN}/itch-setup ${GOBIN}/kitch-setup

