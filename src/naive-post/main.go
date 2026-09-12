// 这是「用 Go 发一个 HTTP POST 请求」的最小可运行示例：
// 把一份数据编码成 JSON，放进请求体里发给服务端，再把响应打出来。
//
// 运行方式（先 cd 到 src/naive-post 目录）：
//
//	go run .
//
// 与 naive-get 相比，只有三处本质区别：
//  1. 方法从 GET 变成 POST；
//  2. 用 encoding/json 把 Go 的值编码成 JSON；
//  3. http.NewRequest 的第三个参数不再是 nil，而是一个可读的请求体。
//
// 想看「真实工程里会怎么改写这份代码」，可以对照 src/usersync。

// package main 是能编译出可执行程序的包名。
package main

// 相比 naive-get，这里多了两个包：
//   - bytes：在内存里操作字节切片，这里用来把 []byte 包装成一个可读的数据流；
//   - encoding/json：JSON 的编解码，标准库自带。
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// main 是程序入口，无参数、无返回值。
func main() {
	const target = "https://jsonplaceholder.typicode.com/posts"

	// 10 * time.Second 和 time.Second * 10 等价。
	// Go 里没有隐式的数值类型转换，必须显式写 time.Second 这样的常量来「带单位」。
	client := &http.Client{Timeout: 10 * time.Second}

	// json.Marshal 把 Go 的值编码成 JSON，返回 ([]byte, error)。
	// 这里的入参是一个 map 字面量：
	//
	//   map[string]any{...}  →  键是 string，值是「任意类型」
	//
	// any 是 interface{} 的别名（Go 1.18 起可直接写 any），
	// 意思是「这里什么类型都能放」，所以 title 放 string、userId 放 int 都没问题。
	// 代价是取出时必须先做类型断言，所以在工程代码里更常用「结构体 + json 标签」，
	// 类型明确、还能被编译器检查（见 src/usersync/internal/jsonplaceholder）。
	//
	// 另外注意：多行写复合字面量时，每一行结尾的逗号都不能省，
	// 包括最后一个元素（"userId": 1,）——这也是 Go 强制的格式要求。
	postData, err := json.Marshal(map[string]any{
		"title":  "My Post",
		"body":   "Content",
		"userId": 1,
	})
	if err != nil {
		panic(err)
	}

	// 第三个参数的类型是 io.Reader —— 只要是「能读出一串字节」的东西都行。
	//
	// 另外还有一个 http.NewRequestWithContext(ctx, method, url, body)，
	// 比上面这个多一个 context 参数，用来做级联取消和超时控制
	// （比如按下 Ctrl+C 时让在途请求立刻返回）。
	// 这个示例是单请求、最小可运行，用不上它，所以这里仍然用 http.NewRequest；
	// 真正需要 context 的地方是有并发、有成串调用的场景，见 src/usersync。
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(postData))
	if err != nil {
		panic(err)
	}

	// 手动设置 Content-Type 头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	// 依旧是那个固定套路：拿到 Body 就立刻 defer 关闭。
	// defer 的调用会推迟到 main 返回前执行，用来释放连接。
	defer resp.Body.Close()

	// 读完整的响应体，得到 []byte。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	// 想看响应头长什么样，把下面这行的注释去掉即可。（函数定义在文件末尾）
	// 它和 naive-get 里的那一份是逐字重复的——这正是「把公共逻辑抽成包」的动机。
	// printHeader(resp)

	// 这里用 fmt.Println：
	// 它是标准库的格式化输出，写到标准输出(stdout)，适合正式代码。
	//
	// 响应体会原样打印 JSON 文本，包括服务端分配好的 id（jsonplaceholder 会返回 id: 101）。
	fmt.Println(string(body))
}

// printHeader 与 naive-get 里的那份功能相同。
// 两个文件里的这个小函数是重复的——这正是「把公共逻辑抽成包」的动机之一，
// 工程版把这类横切逻辑统一收进了 src/usersync/internal/httpx。
func printHeader(resp *http.Response) {
	// resp.Header 是 map[string][]string：字段名 → 该字段的所有取值。
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Println(key, value)
		}
	}
}
