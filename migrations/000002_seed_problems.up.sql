INSERT INTO problems (
    id,
    title,
    description,
    difficulty,
    starter_code,
    test_code
) VALUES
(
    'sum-slice',
    'Sum Slice',
    'Implement a function that returns the sum of all numbers in a slice.',
    'EASY',
    'func Sum(nums []int) int {
    return 0
}',
    'func TestSum(t *testing.T) {
    got := Sum([]int{1, 2, 3})
    want := 6

    if got != want {
        t.Errorf("got %d, want %d", got, want)
    }
}'
),
(
    'reverse-string',
    'Reverse String',
    'Implement a function that returns the input string in reverse order.',
    'EASY',
    'func Reverse(s string) string {
    return ""
}',
    'func TestReverse(t *testing.T) {
    got := Reverse("judge")
    want := "egduj"

    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}'
),
(
    'word-frequency',
    'Word Frequency',
    'Implement a function that counts how many times each word appears in a slice.',
    'MEDIUM',
    'func WordFrequency(words []string) map[string]int {
    return nil
}',
    'func TestWordFrequency(t *testing.T) {
    got := WordFrequency([]string{"go", "judge", "go"})

    if got["go"] != 2 || got["judge"] != 1 {
        t.Errorf("unexpected frequencies: %#v", got)
    }
}'
);