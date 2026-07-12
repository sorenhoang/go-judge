import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { createSubmission, getProblem } from "../api";
import type { Problem } from "../types";

export default function ProblemDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();

  const [problem, setProblem] = useState<Problem | null>(null);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!id) return;

    getProblem(id)
      .then((problem) => {
        setProblem(problem);
        setCode(problem.starter_code);
      })
      .catch((err) => setError(err.message));
  }, [id]);

  async function handleSubmit() {
    if (!problem) return;

    setSubmitting(true);
    setError("");

    try {
      const result = await createSubmission(problem.id, code);
      navigate(`/submissions/${result.submissionId}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to submit");
    } finally {
      setSubmitting(false);
    }
  }

  if (error) return <p>{error}</p>;
  if (!problem) return <p>Loading...</p>;

  return (
    <main>
      <h1>{problem.title}</h1>
      <p>{problem.difficulty}</p>
      <p>{problem.description}</p>

      <textarea
        value={code}
        onChange={(event) => setCode(event.target.value)}
        rows={14}
        style={{ width: "100%", fontFamily: "monospace" }}
      />

      <div style={{ marginTop: 12 }}>
        <button onClick={handleSubmit} disabled={submitting}>
          {submitting ? "Submitting..." : "Submit"}
        </button>
      </div>
    </main>
  );
}