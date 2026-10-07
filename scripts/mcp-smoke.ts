// Actions only: exercise the actual pinned official loader and actual caller keys.
import {readFile, writeFile} from "node:fs/promises";
const input=JSON.parse(await readFile("ci-secrets/mcp-context.json","utf8"));
const sha="6791ae2c8cc1a893b0c6799cf7d24df303e8e0b0";
const response=await fetch(`https://raw.githubusercontent.com/Paca-AI/paca/${sha}/apps/mcp/src/plugin-loader.ts`);
if(!response.ok)throw new Error("Pinned official MCP loader unavailable");
const source=(await response.text()).replace(/^import type .*;\r?\n/gm,"");
await writeFile("/tmp/paca-official-plugin-loader.mjs",new Bun.Transpiler({loader:"ts"}).transformSync(source));
const check=Bun.spawn(["node","scripts/mcp-assert.mjs"],{stdout:"inherit",stderr:"inherit"});
if(await check.exited!==0)throw new Error("Official Node MCP verification failed");
