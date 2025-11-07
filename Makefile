.PHONY: run

main: *.go **/*.go
	go build -o gooj.out

run : main
	sudo ./gooj.out