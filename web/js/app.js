/*
Chess Game Variables, Square Colours, Move History(better to use hashmap or a better struct for this) and API Endpoint
*/
var board = null;
var game = new Chess();
var whiteSquareGrey = 'rgba(52, 152, 219, 0.2)';
var blackSquareGrey = 'rgba(52, 152, 219, 0.3)';
var moveHistory = [];
const API_BASE_URL = window.location.protocol + '//' + 
  (window.location.hostname === 'localhost' ? 'localhost:8080' : window.location.hostname + ':8080');

function removeGreySquares() {
  $('#board .square-55d63').css('background', '');
  $('#board .square-55d63').removeClass('highlight-square');
}
function greySquare(square) {
  var $square = $('#board .square-' + square);
  var background = whiteSquareGrey;
  if ($square.hasClass('black-3c85d')) {
    background = blackSquareGrey;
  }
  $square.css('background', background);
  $square.addClass('highlight-square');
}

/*
Update the move history display
*/
function updateMoveHistory() {
  const history = game.history({ verbose: true });
  let html = '';
  
  for (let i = 0; i < history.length; i += 2) {
    const moveNumber = Math.floor(i / 2) + 1;
    const whiteMove = history[i] ? history[i].san : '';
    const blackMove = history[i + 1] ? history[i + 1].san : '';
    
    html += `
      <div class="move-pair">
        <span class="move-number">${moveNumber}.</span>
        <span class="white-move">${whiteMove}</span>
        ${blackMove ? `<span class="black-move">${blackMove}</span>` : ''}
      </div>
    `;
  }
  
  document.getElementById('move-list').innerHTML = html;
  const moveList = document.getElementById('move-list');
  moveList.scrollTop = moveList.scrollHeight;
}

function getDifficulty() {
  return parseInt(document.getElementById('difficulty').value) || 1600;
}

function getThinkTime() {
  return parseInt(document.getElementById('think-time').value) || 1000;
}

function updateStatus(message, type = 'info') {
  const statusElement = document.getElementById('status');
  statusElement.textContent = message;
  statusElement.classList.remove('status-info', 'status-success', 'status-warning', 'status-error');
  statusElement.classList.add(`status-${type}`);
}

function updateEngineStatus(message) {
  document.getElementById('engine-status').textContent = message;
}

function updateEngineEvaluation(evaluation) {
  document.getElementById('engine-evaluation').textContent = evaluation;
}

function onDragStart(source, piece) {
  if (game.game_over()) return false;
  if (piece.search(/^b/) !== -1) return false;
  if (game.turn() !== 'w') return false;
  return true;
}

async function onDrop(source, target) {
  removeGreySquares();
  var move = game.move({
    from: source,
    to: target,
    promotion: 'q' //Currently we promote to queen, but future probably add a UI for this
  });
  if (move === null) return 'snapback';
  board.position(game.fen());
  updateMoveHistory();
  updateStatus(`You played ${move.san}`, 'info');
  updateEngineStatus('Thinking...');
  if (game.game_over()) {
    handleGameOver();
    return;
  }
  
  try {
    const difficulty = getDifficulty();
    const thinkTime = getThinkTime();
    const response = await fetch(API_BASE_URL + '/move', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ 
        fen: game.fen(),
        elo: difficulty,
        movetime_ms: thinkTime
      })
    });
    
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }
    
    const data = await response.json();
    updateEngineStatus('Ready');
    
    if (data.bestmove && data.bestmove !== '(none)') {
      const from = data.bestmove.substring(0, 2);
      const to = data.bestmove.substring(2, 4);
      const promotion = data.bestmove.length > 4 ? data.bestmove.substring(4, 5) : undefined;
      const engineMove = game.move({ 
        from: from, 
        to: to, 
        promotion: promotion || 'q'
      });
      
      if (engineMove) {
        board.position(game.fen());
        updateMoveHistory();
        updateStatus(`Stockfish played ${engineMove.san}`, 'info');
        if (data.info) {
          const evalMatch = data.info.match(/score cp (-?\d+)/);
          if (evalMatch) {
            const centipawns = parseInt(evalMatch[1]);
            const evaluation = (centipawns / 100).toFixed(1);
            updateEngineEvaluation(`Evaluation: ${evaluation > 0 ? '+' : ''}${evaluation}`);
          } else {
            const mateMatch = data.info.match(/score mate (-?\d+)/);
            if (mateMatch) {
              const mateIn = parseInt(mateMatch[1]);
              updateEngineEvaluation(`Mate in ${Math.abs(mateIn)}`);
            }
          }
        }
        if (game.game_over()) {
          handleGameOver();
        } else if (game.in_check()) {
          updateStatus(`Stockfish played ${engineMove.san} - Check!`, 'warning');
        }
      }
    } else {
      updateStatus('Engine could not find a move', 'error');
    }
    
  } catch (error) {
    console.error('Error:', error);
    updateStatus(`Error: ${error.message}`, 'error');
    updateEngineStatus('Connection Error');
  }
}

/*
Handle all the game over scenarios like checkmate, stalemate, draw, etc.
Implement resignation feature as well in future or maybe then allow the user restart game etc. and delete the current game
*/
function handleGameOver() {
  updateEngineStatus('Game Over');
  updateEngineEvaluation('');
  
  if (game.in_checkmate()) {
    const winner = game.turn() === 'w' ? 'Black' : 'White';
    updateStatus(`Checkmate! ${winner} wins!`, 'success');
  } else if (game.in_draw()) {
    if (game.in_stalemate()) {
      updateStatus('Draw - Stalemate!', 'warning');
    } else if (game.in_threefold_repetition()) {
      updateStatus('Draw - Threefold repetition!', 'warning');
    } else if (game.insufficient_material()) {
      updateStatus('Draw - Insufficient material!', 'warning');
    } else {
      updateStatus('Draw!', 'warning');
    }
  }
}
function onMouseoverSquare(square, piece) {
  if (piece.search(/^w/) === -1 || game.turn() !== 'w') return;
  var moves = game.moves({
    square: square,
    verbose: true
  });
  
  if (moves.length === 0) return;
  greySquare(square);
  for (var i = 0; i < moves.length; i++) {
    greySquare(moves[i].to);
  }
}
function onMouseoutSquare(square, piece) {
  removeGreySquares();
}
function onSnapEnd() {
  board.position(game.fen());
}
function newGame() {
  game.reset();
  board.start();
  moveHistory = [];
  updateStatus('Make your move!', 'info');
  updateEngineStatus('Ready');
  updateEngineEvaluation('Waiting for moves...');
  document.getElementById('move-list').innerHTML = '';
  removeGreySquares();
}
function flipBoard() {
  board.flip();
}

var config = {
  draggable: true,
  position: 'start',
  onDragStart: onDragStart,
  onDrop: onDrop,
  onMouseoutSquare: onMouseoutSquare,
  onMouseoverSquare: onMouseoverSquare,
  onSnapEnd: onSnapEnd,
  pieceTheme: 'img/chesspieces/wikipedia/{piece}.svg', //Using Wikipedia chess pieces,better to use base in future for scalability
  showNotation: true,
  sparePieces: false
};
$(document).ready(function() {
  board = Chessboard('board', config);
  updateStatus('Make your move!', 'info');
  updateEngineStatus('Ready');
  updateEngineEvaluation('Waiting for moves...');
  $(document).keydown(function(e) {
    if (e.key.toLowerCase() === 'n' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      newGame();
    }
    if (e.key.toLowerCase() === 'f' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      flipBoard();
    }
  });
});

const style = document.createElement('style');
style.textContent = `
  .status-info { color: var(--text-dark); }
  .status-success { color: var(--success-color); font-weight: 600; }
  .status-warning { color: var(--warning-color); font-weight: 600; }
  .status-error { color: var(--danger-color); font-weight: 600; }
`;
document.head.appendChild(style);