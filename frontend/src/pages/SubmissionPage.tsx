import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { submissionStreamUrl } from "../api";
import type { Submission } from "../types";

function isTerminal(status: Submission["status"]): boolean {
  return status !== "PENDING" && status !== "RUNNING";
}

export default function SubmissionPage() {
  const { id } = useParams();

  const [submission, setSubmission] = useState<Submission | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!id) return;

    const es = new EventSource(submissionStreamUrl(id));

    es.onmessage = (event) => {
      const result: Submission = JSON.parse(event.data);
      setSubmission(result);
      if (isTerminal(result.status)) {
        es.close();
      }
    };

    es.onerror = () => {
      setError("Lost connection to server");
      es.close();
    };

    return () => es.close();
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