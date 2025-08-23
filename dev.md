# Development setup

- Run docker compose command.  

  ```shell
  docker compose up -d --build
  ```

- Ensure the `go.mod` file exists and defines the module as `github.com/cyokozai/pvectl`.  
- Access the container and run main.go.  

  ```shell
  docker exec -it pvectl-dev go run app/main.go <options>
  ```

- Access the container and run build command.  

  ```shell
  docker exec -it pvectl-dev go build -o app/main.go
  ```
