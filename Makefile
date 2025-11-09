.PHONY: run cmd background

gooj.out: *.go **/*.go
	go build -o gooj.out

run : gooj.out
	sudo ./gooj.out --method=run

cmd : gooj.out
	sudo ./gooj.out --method=cmd

background: gooj.out
	sudo ./gooj.out --method=run --background=true