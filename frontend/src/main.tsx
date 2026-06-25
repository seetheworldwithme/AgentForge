import ReactDOM from 'react-dom/client'
import App from './App'
import './index.css'

// 未使用 React.StrictMode：内置终端的 xterm.js 实例 term.open() 只能调用一次，
// 而 StrictMode 在 dev 下会双调用 effect，导致终端容器二次挂载时为空。
// StrictMode 仅影响 dev 的副作用双调用检查，production 从不启用，移除无运行时影响。
ReactDOM.createRoot(document.getElementById('root')!).render(<App />)
