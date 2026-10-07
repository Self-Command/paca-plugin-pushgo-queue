import {scopePluginCss} from "./scope-css";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import federation from "@originjs/vite-plugin-federation";
import { defineConfig } from "vite";
export default defineConfig({
  plugins:[react(),scopePluginCss(),tailwindcss(), federation({name:"selfcommand_pushgo_queue",filename:"remoteEntry.js",exposes:{"./SettingsTab":"./src/SettingsTab.tsx","./TaskReminderSection":"./src/TaskReminderSection.tsx"},shared:{react:{requiredVersion:"^19.0.0"},"react-dom":{requiredVersion:"^19.0.0"},"@tanstack/react-query":{requiredVersion:"^5.0.0"}}})],
  build:{target:"esnext",minify:false,cssCodeSplit:false}
});
