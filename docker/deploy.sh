
#!/bin/bash

if [ $# -eq 1 ]; then
    COMPOSE_FILE=$1
    STACK_NAME="logmon"
else
    STACK_NAME=$2
fi

if [ ! -f "$COMPOSE_FILE" ]; then
    echo "Error: Compose file '$COMPOSE_FILE' not found!"
    exit 1
fi

if ! docker info > /dev/null 2>&1; then
    echo "Error: Docker is not running!"
    exit 1
fi

if ! docker node ls > /dev/null 2>&1; then
    echo "Error: Docker Swarm is not initialized. Run 'docker swarm init' first."
    exit 1
fi

echo "Deploying stack '$STACK_NAME' using '$COMPOSE_FILE'..."
docker stack deploy -c "$COMPOSE_FILE" "$STACK_NAME"


