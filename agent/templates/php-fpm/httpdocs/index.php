<?php
echo "<h1>__NAME__</h1><p>Xal-Tor-Ka hosting — php ".phpversion()."</p>";
if (getenv('DB_HOST')) {
  echo "<p>DB: <code>".htmlspecialchars(getenv('DB_NAME'))."@".htmlspecialchars(getenv('DB_HOST')).":".htmlspecialchars(getenv('DB_PORT'))."</code> (user <code>".htmlspecialchars(getenv('DB_USER'))."</code>)</p>";
}
