import {defineConfig} from 'vite';
export default defineConfig({base:'/console/',server:{proxy:{'/api':'http://127.0.0.1:8900','/login':'http://127.0.0.1:8900'}}});
