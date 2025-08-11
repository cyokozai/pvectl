# Development setup

- Run docker compose command.  

    ```shell
    docker compose up -d --build
    ```

- Ensure the `go.mod` file exists and defines the module as `app`. If the file is missing, you can initialize it with the following command:

    ```shell
    go mod init app
    ```

- Install the libraries

    ```shell
    go mod tidy
    go get gopkg.in/yaml.v3@latest \
           github.com/google/go-cmp/cmp@latest \
           github.com/Telmate/proxmox-api-go@latest 
    ```

- Access the container

    ```shell
    docker exec -it pvectl-dev go run main.go <options>
    ```

    - Result

        ```shell
          __    _   ___  
         / /\  | | | |_) 
        /_/--\ |_| |_| \_ v1.62.0, built with Go go1.24.5

        watching .
        watching cli
        watching config
        !exclude tmp
        watching version
        building...
        running...
        ```
