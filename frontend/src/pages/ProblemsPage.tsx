import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { listProblems } from "../api";
import type { Problem } from "../types";

export default function ProblemsPage() {
  const [problems, setProblems] = useState<Problem[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    listProblems()
      .then(setProblems)
      .catch((err) => setError(err.message));
  }, []);

  if (error) return <p>{error}</p>;

  return (
    <main>
      <h1>Problems</h1>

      <ul>
        {problems.map((problem) => (
          <li key={problem.id}>
            <Link to={`/problems/${problem.id}`}>
              {problem.title} ({problem.difficulty})
            </Link>
          </li>
        ))}
      </ul>
    </main>
  );
}