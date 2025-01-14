package shadow

import (
	"fmt"
	"testing"
)

func TestUtil(t *testing.T) {
	q1 := "sdfsdf"
	q2 := &q1
	q1 = "wwe"
	fmt.Println(q2)

	a := "/sddf//sdfsdfffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff/sdf/"
	b, _ := encodePath(a, 64)
	fmt.Println(b)

	c := "sdfsdfsfeeeeeeeee141 的说法是色纷纷士大夫大师傅瑟夫随风倒十分士大夫额发生的是发士大夫额士大夫大师傅手动"
	names, _ := splitName(c, 30, 0)
	name, _ := combineName(names)
	fmt.Println(name)
}
