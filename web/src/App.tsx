import reactLogo from "./assets/react.svg";
import "./App.css";

function App() {
  return (
    <main className="container">
      <h1>Welcome to SoloQueue</h1>

      <div className="row">
        <a href="https://vite.dev" target="_blank">
          <img src="/vite.svg" className="logo vite" alt="Vite logo" />
        </a>
        <a href="https://react.dev" target="_blank">
          <img src={reactLogo} className="logo react" alt="React logo" />
        </a>
      </div>
      <p>AI multi-agent collaboration tool</p>
    </main>
  );
}

export default App;
