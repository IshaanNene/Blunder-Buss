#!/bin/bash
set -e

API_URL=${1:-http://localhost:8080}

echo "Test for Blunder-Buss"
echo "API URL: $API_URL"
echo ""

echo "Test for health endpoint..."
HEALTH_RESPONSE=$(curl -s "$API_URL/healthz" || echo "ERROR")
if [[ "$HEALTH_RESPONSE" == *"ok"* ]]; then
    echo "Health check passed"
else
    echo "Health check failed: $HEALTH_RESPONSE"
    exit 1
fi

echo ""
echo "Test for move endpoint with starting position..."
MOVE_RESPONSE=$(curl -s -X POST "$API_URL/move" \
    -H "Content-Type: application/json" \
    -d '{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"}' || echo "ERROR")

if [[ "$MOVE_RESPONSE" == *"bestmove"* ]]; then
    echo "Move request successful"
    echo "Response: $MOVE_RESPONSE"
else
    echo "Move request failed: $MOVE_RESPONSE"
fi

echo ""
echo "Test for move endpoint with custom ELO (1200)..."
MOVE_RESPONSE_ELO=$(curl -s -X POST "$API_URL/move" \
    -H "Content-Type: application/json" \
    -d '{"fen":"rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1","elo":1200,"movetime_ms":500}' || echo "ERROR")

if [[ "$MOVE_RESPONSE_ELO" == *"bestmove"* ]]; then
    echo "ELO move request successful"
    echo "Response: $MOVE_RESPONSE_ELO"
else
    echo "ELO move request failed: $MOVE_RESPONSE_ELO"
fi

echo ""
echo "Test for invalid request handling..."
INVALID_RESPONSE=$(curl -s -X POST "$API_URL/move" \
    -H "Content-Type: application/json" \
    -d '{"invalid":"data"}' || echo "ERROR")

if [[ "$INVALID_RESPONSE" == *"missing fen"* ]]; then
    echo "Invalid request handled correctly"
else
    echo "Invalid request response: $INVALID_RESPONSE"
fi

echo ""
echo "Test for CORS headers..."
CORS_RESPONSE=$(curl -s -I -X OPTIONS "$API_URL/move" \
    -H "Origin: http://localhost:3000" \
    -H "Access-Control-Request-Method: POST" \
    -H "Access-Control-Request-Headers: Content-Type" || echo "ERROR")

if [[ "$CORS_RESPONSE" == *"Access-Control-Allow-Origin"* ]]; then
    echo "CORS headers present"
else
    echo "CORS headers missing"
fi

echo ""
echo "API Test for completed!"

if command -v hey &> /dev/null; then
    echo ""
    echo "Running performance test (100 requests)..."
    hey -n 100 -c 5 -m POST -H "Content-Type: application/json" \
        -d '{"fen":"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"}' \
        "$API_URL/move"
else
    echo ""
    echo "Install 'hey' for performance Test for: go install github.com/rakyll/hey@latest"
fi