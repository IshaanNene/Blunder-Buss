#!/bin/bash

set -e

ACTION=${1:-up}

case $ACTION in
    "up")
        echo "Starting BlunderBuss locally..."
        docker compose -f docker-compose.local.yml up --build -d
        echo ""
        echo "Services started successfully!"
        echo ""
        echo "Access the application:"
        echo "  Web Interface: http://localhost:3000"
        echo "  API: http://localhost:8080"
        echo "  API Health: http://localhost:8080/healthz"
        echo ""
        echo "Service endpoints:"
        echo "  Redis: localhost:6379"
        echo "  Stockfish: localhost:4000"
        echo ""
        echo "Useful commands:"
        echo "  View logs: ./scripts/deploy-local.sh logs"
        echo "  Check status: ./scripts/deploy-local.sh status"
        echo "  Stop services: ./scripts/deploy-local.sh down"
        ;;
    
    "down")
        echo "Stopping BlunderBuss..."
        docker compose -f docker-compose.local.yml down
        echo "Services stopped successfully!"
        ;;
    
    "logs")
        echo "Showing logs for all services..."
        docker compose -f docker-compose.local.yml logs -f
        ;;
    
    "status")
        echo "Service status:"
        docker compose -f docker-compose.local.yml ps
        echo ""
        echo "Health checks:"
        echo -n "API Health: "
        curl -s http://localhost:8080/healthz || echo "Not responding"
        echo -n "Web Interface: "
        curl -s -I http://localhost:3000 | head -n 1 || echo "Not responding"
        echo -n "Redis: "
        redis-cli -p 6379 ping 2>/dev/null || echo "Not responding"
        ;;
    
    *)
        echo "Usage: $0 [up|down|logs|status]"
        echo ""
        echo "Commands:"
        echo "  up     - Start all services (default)"
        echo "  down   - Stop all services"
        echo "  logs   - Show logs for all services"
        echo "  status - Show service status and health"
        exit 1
        ;;
esac