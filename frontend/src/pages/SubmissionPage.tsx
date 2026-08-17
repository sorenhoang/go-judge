import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { getSubmission } from "../api";
import type { Submission } from "../types";

export default function SubmissionPage() {
  const { id } = useParams();

  const [submission, setSubmission] = useState<Submission | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!id) return;

    let cancelled = false;

    function poll() {
      getSubmission(id!)
        .then((result) => {
          if (cancelled) return;
          setSubmission(result);
          if (result.status === "PENDING" || result.status === "RUNNING") {
            timer = setTimeout(poll, 1000);
          }
        })
        .catch((err) => {
          if (!cancelled) setError(err.message);
        });
    }

    let timer: ReturnType<typeof setTimeout>;
    poll();

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [id]);

  if (error) return <p>{error}</p>;
  if (!submission) return <p>Loading...</p>;

  return (
    <main>
      <h1>Submission</h1>

      <p>Status: {submission.status}</p>
      <p>
        Tests: {submission.passed_tests}/{submission.total_tests}
      </p>

      <h2>Output</h2>
      <pre style={{ whiteSpace: "pre-wrap" }}>
        {submission.output || "(no output)"}
      </pre>

      <h2>Code</h2>
      <pre style={{ whiteSpace: "pre-wrap" }}>{submission.code}</pre>
    </main>
  );
}