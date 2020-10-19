build:
	GOOS=linux go build -o bin/kvass cmd/kvass/*.go
image: build
	docker build -t ccr.ccs.tencentyun.com/tkeimages/kvass:v0.0.1 .
release: image
	docker push ccr.ccs.tencentyun.com/tkeimages/kvass:v0.0.1
clean:
	rm -fr bin
protoc:
	protoc --go_out=plugins=grpc:./ shard.proto  -I ./
