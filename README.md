Forked off of:

 https://github.com/foundriesio/compose-publish

to also include [runc](https://github.com/opencontainers/runc) and
[crun](https://github.com/containers/crun) compliant
[specs](https://github.com/opencontainers/runtime-spec/blob/master/spec.md).

## Why

Docker-compose is amazing, but it left off one key piece that pure docker
solved: distribution. capp-pub aims to address this. It basically captures
all the information needed to run the exact docker-compose file on another
system.

But while, we were at it, once you have all this information, its not
a big leap to produce a *really* lightweight tool to run docker-compose
applications without Docker(or Golang). This tool looks at docker-compose
and each container and produces runc specs that can be executed by the
crun project.

Setting things up to run takes a little help so a complimentary tool,
capp-run, was created that's capable of running the bundles created by this
tool.

The goal is to run nearly any docker-compose file the same as Docker would.
We'll never get that far, but should be able to cover most normal uses. The
hope is this compromise is worth the benefit of being able to run this all
with capp-run/crun.

## Quickstart

~~~

$ make build
$ cd example
$ ../build/compose-publish --dryrun foo:bar
~~~

That will create a tarball `compose-bundle.tgz`. This can be used by capp-run.

## What's Missing

Lots of stuff is missing. The `internal/runc.go` is trying to create specs
as close to Docker's [oci_linux.go](https://github.com/moby/moby/blob/a602b052a9c285e9659e9ce007f2aa7f0a73812f/daemon/oci_linux.go)
as possible. The `RuncSpec` function has some TODOs of what it hasn't
implemented. As function is added, the `example/docker-compose.yml` file
is updated with a test to help enumerate and test what is possible.

Big TODO's include:
 * reject things we don't support so its obvious what will run
 * complex network
 * complex port
 * some advanced volume options
