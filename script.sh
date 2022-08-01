wg-quick up wg0 

while [ true ];
do
  x=$(wg | wc -l)
  [ $x -eq 0 ] && exit 1
  sleep 1;
done
