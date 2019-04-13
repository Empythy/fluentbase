#!/bin/bash
set -e

cd $(dirname "${BASH_SOURCE[0]}")
OD="$(pwd)"

VERSION=1.16
PROTECTED_MODE="no"

# Hardcode some values to the core package
LDFLAGS="$LDFLAGS -X github.com/gvsafronov/fluentbase/core.Version=${VERSION}"
if [ -d ".git" ]; then
	LDFLAGS="$LDFLAGS -X github.com/gvsafronov/fluentbase/core.GitSHA=$(git rev-parse --short HEAD)"
fi
LDFLAGS="$LDFLAGS -X github.com/gvsafronov/fluentbase/core.BuildTime=$(date +%FT%T%z)"
if [ "$PROTECTED_MODE" == "no" ]; then
	LDFLAGS="$LDFLAGS -X github.com/gvsafronov/fluentbase/core.ProtectedMode=no"
fi

if [ "$1" == "update-version" ]; then
	# update the versions in the README.md
	sed -i '' "s/version-[0-9]*\.[0-9]*\.[0-9]*/version-$VERSION/g" README.md
	exit
fi

if [ "$1" == "travis-docker-push" ]; then
    # GIT_VERSION - always the last verison number, like 1.12.1.
    export GIT_VERSION=$(git describe --tags --abbrev=0)  
    # GIT_COMMIT_SHORT - the short git commit number, like a718ef0.
    export GIT_COMMIT_SHORT=$(git rev-parse --short HEAD)
    # DOCKER_REPO - the base repository name to push the docker build to.
    export DOCKER_REPO=$DOCKER_USER/fluentbase

    if [ "$TRAVIS_PULL_REQUEST" != "false" ]; then 
        # never push from a pull request
        echo "Not pushing, on a PR or not running in Travis CI"
    elif [ "$TRAVIS_BRANCH" != "master" ]; then
        # only the master branch will work
        echo "Not pushing, not on master"
    else
        push(){
            docker tag $DOCKER_REPO:$GIT_COMMIT_SHORT $DOCKER_REPO:$1
            docker push $DOCKER_REPO:$1
            echo "Pushed $DOCKER_REPO:$1"
        }
        # docker login
        echo $DOCKER_PASSWORD | docker login -u $DOCKER_LOGIN --password-stdin
        # build the docker image
        docker build -f Dockerfile -t $DOCKER_REPO:$GIT_COMMIT_SHORT .
        if [ "$(curl -s https://hub.docker.com/v2/repositories/$DOCKER_REPO/tags/$GIT_VERSION/ | grep "$GIT_VERSION" | grep "repository")" == "" ]; then
            # push the newest tag
            push "$GIT_VERSION"
            push "latest"
        fi
        push "edge"
    fi
    exit
fi


# Check go install
if [ "$(which go)" == "" ]; then
	echo "error: Go is not installed. Please download and follow installation instructions at https://golang.org/dl to continue."
	exit 1
fi

# Check go version
GOVERS="$(go version | cut -d " " -f 3)"
if [ "$GOVERS" != "devel" ]; then
	vercomp () {
		if [[ $1 == $2 ]]
		then
			echo "0"
			return
		fi
		local IFS=.
		local i ver1=($1) ver2=($2)
		# fill empty fields in ver1 with zeros
		for ((i=${#ver1[@]}; i<${#ver2[@]}; i++))
		do
			ver1[i]=0
		done
		for ((i=0; i<${#ver1[@]}; i++))
		do
			if [[ -z ${ver2[i]} ]]
			then
				# fill empty fields in ver2 with zeros
				ver2[i]=0
			fi
			if ((10#${ver1[i]} > 10#${ver2[i]}))
			then
				echo "1"
				return
			fi
			if ((10#${ver1[i]} < 10#${ver2[i]}))
			then
				echo "-1"
				return
			fi
		done
		echo "0"
		return
	}
	GOVERS="${GOVERS:2}"
	EQRES=$(vercomp "$GOVERS" "1.7")  
	if [ "$EQRES" == "-1" ]; then
		  echo "error: Go '1.7' or greater is required and '$GOVERS' is currently installed. Please upgrade Go at https://golang.org/dl to continue."	
		  exit 1
	fi
fi

package(){
	echo Packaging $1 Binary
	bdir=fluentbase-${VERSION}-$2-$3
	rm -rf packages/$bdir && mkdir -p packages/$bdir
	GOOS=$2 GOARCH=$3 ./build.sh

	if [ "$2" == "linux" ]; then
		tar -zcf $bdir.tar.gz $bdir
	else
		zip -r -q $bdir.zip $bdir
	fi
	rm -rf $bdir
	cd ..
}

if [ "$1" == "package" ]; then
	rm -rf packages/
	package "Mac" "darwin" "amd64"
	package "Linux" "linux" "amd64"
	package "FreeBSD" "freebsd" "amd64"
	package "ARM" "linux" "arm"
	package "ARM64" "linux" "arm64"
	exit
fi

# temp directory for storing isolated environment.
TMP="$(mktemp -d -t fluentbase.XXXX)"
function rmtemp {
	rm -rf "$TMP"
}
trap rmtemp EXIT

if [ "$NOLINK" != "1" ]; then
    # symlink root to isolated directory
	mkdir -p "$TMP/go/src/github.com/gvsafronov"
    ln -s $OD "$TMP/go/src/github.com/gvsafronov/fluentbase"
    export GOPATH="$TMP/go"
	cd "$TMP/go/src/github.com/gvsafronov/fluentbase"
fi

# generate the core package
core/gen.sh

export CGO_ENABLED=0

# build and store objects into original directory.
go build -ldflags "$LDFLAGS -extldflags '-static'" -o "$OD/fluentbase-server" cmd/fluentbase-server/*.go
go build -ldflags "$LDFLAGS -extldflags '-static'" -o "$OD/fluentbase-cli" cmd/fluentbase-cli/*.go
go build -ldflags "$LDFLAGS -extldflags '-static'" -o "$OD/fluentbase-luamemtest" cmd/fluentbase-luamemtest/*.go

# test if requested
if [ "$1" == "test" ]; then
	$OD/fluentbase-server -p 9876 -d "$TMP" -q &
	PID=$!
	function testend {
		kill $PID &
	}
	trap testend EXIT
	cd tests && go test && cd ..
	go test $(go list ./... | grep -v /vendor/ | grep -v /tests)
fi

# cover if requested
if [ "$1" == "cover" ]; then
	$OD/fluentbase-server -p 9876 -d "$TMP" -q &
	PID=$!
	function testend {
		kill $PID &
	}
	trap testend EXIT
	go test -cover $(go list ./... | grep -v /vendor/)
fi


