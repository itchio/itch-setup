module github.com/itchio/itch-setup

go 1.12

require (
	github.com/BurntSushi/toml v0.3.1 // indirect
	github.com/alecthomas/template v0.0.0-20160405071501-a0175ee3bccc // indirect
	github.com/alecthomas/units v0.0.0-20151022065526-2efee857e7cf // indirect
	github.com/cloudfoundry-attic/jibber_jabber v0.0.0-20151120183258-bcc4c8345a21
	github.com/cloudfoundry/jibber_jabber v0.0.0-20151120183258-bcc4c8345a21 // indirect
	github.com/dchest/safefile v0.0.0-20151022103144-855e8d98f185
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/google/uuid v1.1.1
	github.com/gotk3/gotk3 v0.0.0-20190620081259-6dcdf9e5c51e
	github.com/itchio/go-itchio v0.0.0-20190703105933-6cc0976392aa
	github.com/itchio/headway v0.0.0-20191015112415-46f64dd4d524
	github.com/itchio/httpkit v0.0.0-20200304092139-56c2e1e88c9b
	github.com/itchio/husk v0.0.0-00010101000000-000000000000
	github.com/itchio/kompress v0.0.0-20190703125833-0b2a6b182782 // indirect
	github.com/itchio/lake v0.0.0-20190703103538-f71861a8a3eb
	github.com/itchio/ox v0.0.0-20190705170940-1e1b8248fbc5
	github.com/itchio/savior v0.0.0-20190702184736-b8b849654d01
	github.com/itchio/wharf v0.0.0-20190703124244-659fddc29012
	github.com/lxn/walk v0.0.0-20190619151032-86d8802c197a
	github.com/lxn/win v0.0.0-20190618153233-9c04a4e8d0b8
	github.com/mitchellh/go-ps v0.0.0-20170309133038-4fdf99ab2936
	github.com/onsi/ginkgo v1.13.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/scjalliance/comshim v0.0.0-20190308082608-cf06d2532c4e
	github.com/skratchdot/open-golang v0.0.0-20190402232053-79abb63cd66e
	golang.org/x/sys v0.0.0-20200519105757-fe76b779f299
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
	gopkg.in/alecthomas/kingpin.v2 v2.2.6
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
)

replace github.com/itchio/husk => ./node_modules/@itchio/husk/artifacts
