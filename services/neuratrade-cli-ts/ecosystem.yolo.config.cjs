// YOLO 10%/day attempt — 10x leverage, 1m, pause 0, no chop gate
// WARNING: liquidates at ~9.5% adverse move, expects ~ -30% DD/week. Demo only.
const path = require("node:path");
const fs = require("node:fs");
function loadEnv(f){const e={};if(!fs.existsSync(f))return e;for(const l of fs.readFileSync(f,"utf8").split("\n")){const t=l.trim();if(!t||t.startsWith("#"))continue;const i=t.indexOf("=");if(i<=0)continue;let k=t.slice(0,i).trim(),v=t.slice(i+1).trim();if(v.startsWith('"')&&v.endsWith('"'))v=v.slice(1,-1);e[k]=v}return e}
const rootEnv=loadEnv(path.join(__dirname,"../..",".env"));
const home=(rootEnv.NEURATRADE_HOME||`${process.env.HOME}/.neuratrade`).replace("${HOME}",process.env.HOME);
const cli=__dirname;
module.exports={apps:[{
  name:"neuratrade-bybit-book", // reuse existing name so pm2 replaces it
  script:"bun",args:["run","index.ts","scalp","paper-trade",
    "--exchange","bybit-futures","--timeframe","1m","--futures","--live","--strategy-type","grid",
    "--trend-filter-period","0","--fee","0.06","--slippage-bps","2",
    "--leverage","10","--capital","50","--min-capital","50",
    "--max-position-size-pct","100","--max-drawdown-pct","20","--max-daily-loss-pct","10",
    "--grid-step-pct","0.3","--grid-max-grids","2","--grid-pause-after-loss-bars","0",
    "--target-ratio","1","--chop-gate-adx","0","--no-watchlist","--iterations","0","--interval","60"],
  cwd:cli,env:{...rootEnv,NEURATRADE_HOME:home,NODE_ENV:"production",BYBIT_USE_TESTNET:"true"},
  autorestart:true,max_restarts:10,restart_delay:5_000,
  out_file:path.join(home,"logs","bybit-book.out.log"),error_file:path.join(home,"logs","bybit-book.err.log"),
  max_size:"50M",retain:5,merge_logs:true,time:true
}]};
