UPX_LEVEL?=-1

ifeq (GOOS,386)
GOBIN:=${GOPATH}/bin/windows_386/
else
GOBIN:=${GOPATH}/bin/
endif

all:
	windres -o itchSetup.syso itchSetup.rc
	go get -v -x -ldflags="-H windowsgui"
	upx ${UPX_LEVEL} ${GOBIN}/itchSetup.exe
