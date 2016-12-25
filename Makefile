UPX_LEVEL ?= -1

ifeq ($(OS),Windows_NT)
SETUP_OS:=Windows
ifeq (GOOS,386)
GOBIN:=${GOPATH}/bin/windows_386
else
GOBIN:=${GOPATH}/bin
endif
else
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
SETUP_OS:=Linux
GOBIN:=${GOPATH}/bin
else
SETUP_OS:=Unknown
endif
endif

all:
	-@make $(SETUP_OS)

Linux:
	go get -v -x -tags gtk_3_18
	upx ${UPX_LEVEL} ${GOBIN}/itchSetup

Windows:
	windres -o itchSetup.syso itchSetup.rc
	go get -v -x -ldflags="-H windowsgui"
	upx ${UPX_LEVEL} ${GOBIN}/itchSetup.exe
