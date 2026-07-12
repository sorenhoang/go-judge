import { Link, Navigate, Route, Routes } from "react-router-dom";
import ProblemsPage from "./pages/ProblemsPage";
import ProblemDetailPage from "./pages/ProblemDetailPage";
import SubmissionPage from "./pages/SubmissionPage";

export default function App() {
  return (
    <div style={{ maxWidth: 960, margin: "0 auto", padding: 24 }}>
      <header style={{ marginBottom: 24 }}>
        <Link to="/problems">Go Judge</Link>
      </header>

      <Routes>
        <Route path="/" element={<Navigate to="/problems" replace />} />
        <Route path="/problems" element={<ProblemsPage />} />
        <Route path="/problems/:id" element={<ProblemDetailPage />} />
        <Route path="/submissions/:id" element={<SubmissionPage />} />
      </Routes>
    </div>
  );
}