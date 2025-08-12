# Development setup

- Run docker compose command.  

  ```shell
  docker compose up -d --build
  ```

- Ensure the `go.mod` file exists and defines the module as `github.com/cyokozai/pvectl`.  
  If the file is missing, you can initialize it with the following command and installing the required libraries:

  ```shell
  docker exec -it pvectl-dev sh -c """
  go mod init github.com/cyokozai/pvectl && \
  go get gopkg.in/yaml.v3@latest && \
  go get github.com/google/go-cmp/cmp@latest && \
  go get github.com/Telmate/proxmox-api-go@latest && \
  go install golang.org/x/tools/cmd/goimports@latest && \
  go install golang.org/x/lint/golint@latest
  """
  ```

- Access the container and run main.go. 

  ```shell
  docker exec -it pvectl-dev go run main.go <options>
  ```

- Access the container and run build command.  

  ```shell
  docker exec -it pvectl-dev go build -o main.go
  ```
