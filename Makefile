all: 
	@./build.sh
clean:
	rm -f fluentbase-server
	rm -f fluentbase-cli
	rm -f fluentbase-benchmark
	rm -f fluentbase-luamemtest
test:
	@./build.sh test
cover:
	@./build.sh cover
install: all
	cp fluentbase-server /usr/local/bin
	cp fluentbase-cli /usr/local/bin
	cp fluentbase-benchmark /usr/local/bin
uninstall: 
	rm -f /usr/local/bin/fluentbase-server
	rm -f /usr/local/bin/fluentbase-cli
	rm -f /usr/local/bin/fluentbase-benchmark
package:
	@./build.sh package
