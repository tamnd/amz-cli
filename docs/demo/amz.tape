# Demo tape for amz. Rendered with ascii-gif (github.com/tamnd/ascii-gif),
# which supplies the window chrome and theme; this file is just the action.
#
#   ascii-gif render docs/demo/amz.tape -o docs/static/demo.gif
#
# amz must be on PATH inside the recording shell.

Hide
Type "export PS1='$ ' PATH=/Users/apple/bin:$PATH"
Enter
Type "clear"
Enter
Show

Sleep 500ms
Type "amz bestsellers electronics -n 5 --fields rank,title,price,rating"
Sleep 800ms
Enter
Sleep 3s

Type "amz bestsellers electronics -n 6 -o url"
Sleep 800ms
Enter
Sleep 2.5s

Type "amz bestsellers electronics -n 4 -o jsonl | jq '{rank, price, asin}'"
Sleep 800ms
Enter
Sleep 3s
